package diagnostics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/gofrs/uuid"
)

type NotificationLogs interface {
	Logs(context.Context, config.Service, bool, int64, io.Writer) error
}

// BusinessSmoke создаёт собственную бронь и освобождает номер отменой в конце проверки.
func BusinessSmoke(ctx context.Context, cfg config.Config, logs NotificationLogs, out io.Writer) error {
	services, err := cfg.Select([]string{"api-gateway", "booking", "check-in", "hotels", "review", "notification"})
	if err != nil {
		return err
	}
	var notification config.Service
	for _, service := range services {
		if service.Name == "notification" {
			notification = service
		}
	}
	actor, err := uuid.NewV4()
	if err != nil {
		return err
	}
	transport := &http.Transport{
		Proxy: nil,
	}
	defer transport.CloseIdleConnections()
	scenario := &businessScenario{
		base: "http://127.0.0.1:" + strconv.Itoa(cfg.Runtime.Ports.HTTP),
		client: &http.Client{
			Transport: transport,
			Timeout:   10 * time.Second,
		},
		actor: actor.String(),
		out:   out,
	}
	bookingID, err := scenario.run(ctx)
	if err != nil {
		return err
	}
	if err := awaitBusiness(ctx, func() (bool, error) {
		var output bytes.Buffer
		if err := logs.Logs(ctx, notification, false, 1000, &output); err != nil {
			return false, err
		}
		return notificationDelivered(&output, bookingID)
	}); err != nil {
		return fmt.Errorf("уведомления для брони %s: %w", bookingID, err)
	}
	fmt.Fprintln(out, "OK Notification: получены три push-лога с разными correlation_id")
	fmt.Fprintf(out, "OK бизнес-сценарий завершён: booking_id=%s; номер освобождён\n", bookingID)
	return nil
}

func (s *businessScenario) run(ctx context.Context) (bookingID string, err error) {
	room, input, err := s.freeRoom(ctx)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(s.out, "OK справочник Hotels/Review и доступность: room_id=%s, номер=%s, тип=%s\n", room.ID, room.Number, room.RoomType)
	fmt.Fprintf(s.out, "Проверка: actor_id=%s, idempotency_key=%s\n", s.actor, input.IdempotencyKey)
	var created bookingResponse
	if err := s.request(ctx, http.MethodPost, "/v1/bookings", "GUEST", input, &created, http.StatusOK); err != nil {
		return "", err
	}
	bookingID = created.Booking.ID
	fmt.Fprintf(s.out, "Создана тестовая бронь: booking_id=%s\n", bookingID)
	canceled := false
	defer func() {
		if err == nil || canceled || bookingID == "" {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if cleanupErr := s.transition(cleanup, bookingID, "cancel", "CANCELED", "CANCELED"); cleanupErr != nil {
			err = fmt.Errorf("%w; бронь %s не удалось отменить: %v", err, bookingID, cleanupErr)
			return
		}
		fmt.Fprintf(s.out, "Тестовая бронь %s отменена после ошибки\n", bookingID)
	}()
	if _, err := uuid.FromString(bookingID); err != nil {
		return bookingID, fmt.Errorf("Booking вернул некорректный UUID: %w", err)
	}
	if created.Booking.Status != "CONFIRMED" || created.Booking.RoomID != room.ID || created.Booking.RoomNumber != room.Number || created.Booking.RoomType != room.RoomType {
		return bookingID, fmt.Errorf("созданная бронь не соответствует номеру или не подтверждена: %+v", created.Booking)
	}
	var replay bookingResponse
	if err := s.request(ctx, http.MethodPost, "/v1/bookings", "GUEST", input, &replay, http.StatusOK); err != nil {
		return bookingID, err
	}
	if replay.Booking != created.Booking {
		return bookingID, fmt.Errorf("повтор ключа изменил бронь: %+v", replay.Booking)
	}
	fmt.Fprintln(s.out, "OK создание CONFIRMED и идемпотентный повтор")

	overlap := input
	overlapKey, err := uuid.NewV4()
	if err != nil {
		return bookingID, err
	}
	overlap.IdempotencyKey = overlapKey.String()
	var businessError struct {
		Code int `json:"code"`
	}
	if err := s.request(ctx, http.MethodPost, "/v1/bookings", "GUEST", overlap, &businessError, http.StatusConflict); err != nil {
		return bookingID, err
	}
	if businessError.Code != 9 {
		return bookingID, fmt.Errorf("пересечение должно вернуть FailedPrecondition (9), получено %d", businessError.Code)
	}
	fmt.Fprintln(s.out, "OK пересечение отклонено с HTTP 409 / FailedPrecondition")

	filter := url.Values{
		"starts_at": {input.StartsAt},
		"ends_at":   {input.EndsAt},
	}
	if err := awaitBusiness(ctx, func() (bool, error) {
		var arrivals struct {
			CheckIns []scenarioCheckIn `json:"checkins"`
		}
		if err := s.request(ctx, http.MethodGet, "/v1/check-in/bookings?"+filter.Encode(), "RECEPTION", nil, &arrivals, http.StatusOK); err != nil {
			return false, err
		}
		for _, checkIn := range arrivals.CheckIns {
			if checkIn.BookingID == bookingID {
				return checkIn.Status == "EXPECTED", nil
			}
		}
		return false, nil
	}); err != nil {
		return bookingID, fmt.Errorf("появление брони в Check-in: %w", err)
	}
	fmt.Fprintln(s.out, "OK booking-confirmed доставлено в Check-in: EXPECTED")
	if err := s.transition(ctx, bookingID, "activate", "CHECKED_IN", "ACTIVE"); err != nil {
		return bookingID, err
	}
	fmt.Fprintln(s.out, "OK request/reply активации: Booking ACTIVE, Check-in CHECKED_IN")
	if err := s.transition(ctx, bookingID, "cancel", "CANCELED", "CANCELED"); err != nil {
		return bookingID, err
	}
	canceled = true
	available, err := s.available(ctx, room, input)
	if err != nil {
		return bookingID, err
	}
	if !available {
		return bookingID, fmt.Errorf("после отмены номер %s не вернулся в доступные", room.ID)
	}
	fmt.Fprintln(s.out, "OK request/reply отмены: оба сервиса CANCELED, номер снова доступен")
	return bookingID, nil
}

func (s *businessScenario) transition(ctx context.Context, bookingID, action, checkInStatus, bookingStatus string) error {
	key, err := uuid.NewV4()
	if err != nil {
		return err
	}
	input := map[string]string{
		"idempotency_key": key.String(),
	}
	// Повторяется один ключ: читаем результат асинхронной операции без нового события.
	err = awaitBusiness(ctx, func() (bool, error) {
		var result checkInResponse
		if err := s.request(ctx, http.MethodPost, "/v1/check-in/bookings/"+bookingID+"/"+action, "RECEPTION", input, &result, http.StatusOK); err != nil {
			return false, err
		}
		if result.CheckIn.BookingID != bookingID {
			return false, fmt.Errorf("Check-in вернул другую бронь")
		}
		if result.CheckIn.Failure != "" {
			return false, fmt.Errorf("Check-in: %s", result.CheckIn.Failure)
		}
		if result.CheckIn.Status != checkInStatus {
			return false, nil
		}
		var booking bookingResponse
		if err := s.request(ctx, http.MethodGet, "/v1/bookings/"+bookingID, "GUEST", nil, &booking, http.StatusOK); err != nil {
			return false, err
		}
		return booking.Booking.ID == bookingID && booking.Booking.Status == bookingStatus, nil
	})
	if err != nil {
		return fmt.Errorf("%s брони %s: %w", action, bookingID, err)
	}
	return nil
}

func awaitBusiness(ctx context.Context, check func() (bool, error)) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		ready, err := check()
		if err != nil || ready {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("не дождались результата: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func notificationDelivered(reader io.Reader, bookingID string) (bool, error) {
	wanted := map[string]bool{
		"Бронирование подтверждено": false,
		"Добро пожаловать!":         false,
		"Бронирование отменено":     false,
	}
	correlations := map[string]string{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var entry struct {
			BookingID     string `json:"booking_id"`
			CorrelationID string `json:"correlation_id"`
			Provider      string `json:"provider"`
			Channel       string `json:"channel"`
			Message       string `json:"msg"`
			Text          string `json:"text"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.BookingID != bookingID {
			continue
		}
		if _, ok := wanted[entry.Message]; !ok || entry.Provider != "stdout" || entry.Channel != "push" {
			continue
		}
		id, err := uuid.FromString(entry.CorrelationID)
		if err != nil || id == uuid.Nil || entry.Text == "" {
			return false, fmt.Errorf("уведомление %q не содержит текста или correlation_id UUID", entry.Message)
		}
		if previous, ok := correlations[entry.CorrelationID]; ok && previous != entry.Message {
			return false, fmt.Errorf("разные события имеют одинаковый correlation_id %s", entry.CorrelationID)
		}
		correlations[entry.CorrelationID] = entry.Message
		wanted[entry.Message] = true
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	for _, delivered := range wanted {
		if !delivered {
			return false, nil
		}
	}
	return true, nil
}
