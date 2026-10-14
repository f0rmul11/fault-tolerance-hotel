package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/f0rmul11/fault-tolerance-hotel/hotelctl/internal/config"
	"github.com/gofrs/uuid"
)

func TestBusinessScenario(t *testing.T) {
	for _, test := range []struct {
		name      string
		fault     string
		wantError string
	}{
		{
			name: "создание повтор запрет пересечения активация отмена",
		},
		{
			name:      "повтор с другим ID считается ошибкой и отменяет тестовую бронь",
			fault:     "replay",
			wantError: "повтор ключа изменил бронь",
		},
		{
			name:      "бизнес отказ активации не маскируется и бронь отменяется",
			fault:     "activation",
			wantError: "HTTP 409",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			bookingID := uuid.Must(uuid.NewV4()).String()
			booking := scenarioBooking{
				ID:         bookingID,
				RoomID:     "7",
				RoomNumber: "305",
				RoomType:   "Suite",
				Status:     "CONFIRMED",
				Version:    1,
			}
			key := ""
			canceled := false
			activated := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := uuid.FromString(r.Header.Get("X-Request-Id")); err != nil {
					t.Error("нет request_id UUID")
				}
				role := "GUEST"
				if strings.HasPrefix(r.URL.Path, "/v1/check-in/") {
					role = "RECEPTION"
				}
				if r.Header.Get("X-Demo-Role") != role {
					t.Errorf("неверная роль для %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/v1/hotels":
					io.WriteString(w, `{"hotels":[{"id":"hotel"}]}`)
				case "/v1/hotels/hotel/rooms":
					// Protobuf JSON допускает int64 строкой; клиент понимает и число.
					io.WriteString(w, `{"rooms":[{"id":"7","hotel_id":"hotel","number":"305","room_type":"Suite","min_duration_seconds":"43200","max_duration_seconds":604800}]}`)
				case "/v1/rooms/availability":
					if key != "" && !canceled {
						io.WriteString(w, `{"room_ids":[]}`)
						return
					}
					io.WriteString(w, `{"room_ids":["7"]}`)
				case "/v1/bookings":
					var input createBookingRequest
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					if key != "" && key != input.IdempotencyKey {
						w.WriteHeader(http.StatusConflict)
						io.WriteString(w, `{"code":9,"message":"Номер занят"}`)
						return
					}
					response := booking
					if key != "" && test.fault == "replay" {
						response.ID = uuid.Must(uuid.NewV4()).String()
					}
					key = input.IdempotencyKey
					json.NewEncoder(w).Encode(bookingResponse{Booking: response})
				case "/v1/bookings/" + bookingID:
					json.NewEncoder(w).Encode(bookingResponse{Booking: booking})
				case "/v1/check-in/bookings":
					if r.URL.Query().Get("starts_at") == "" || r.URL.Query().Get("ends_at") == "" {
						t.Error("нет фильтра диапазона прибытия")
					}
					json.NewEncoder(w).Encode(map[string]any{
						"checkins": []scenarioCheckIn{{
							BookingID: bookingID,
							Status:    "EXPECTED",
						}},
					})
				case "/v1/check-in/bookings/" + bookingID + "/activate":
					if test.fault == "activation" {
						w.WriteHeader(http.StatusConflict)
						io.WriteString(w, `{"code":9,"message":"Отказ активации"}`)
						return
					}
					activated = true
					booking.Status = "ACTIVE"
					json.NewEncoder(w).Encode(checkInResponse{CheckIn: scenarioCheckIn{
						BookingID: bookingID,
						Status:    "CHECKED_IN",
					}})
				case "/v1/check-in/bookings/" + bookingID + "/cancel":
					canceled = true
					booking.Status = "CANCELED"
					json.NewEncoder(w).Encode(checkInResponse{CheckIn: scenarioCheckIn{
						BookingID: bookingID,
						Status:    "CANCELED",
					}})
				default:
					t.Errorf("неожиданный путь %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			scenario := &businessScenario{
				base:   server.URL,
				client: server.Client(),
				actor:  uuid.Must(uuid.NewV4()).String(),
				out:    io.Discard,
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			var id string
			var err error
			if test.fault == "" {
				cfg := config.Config{
					Runtime: config.Runtime{
						Ports: config.Ports{
							HTTP: server.Listener.Addr().(*net.TCPAddr).Port,
						},
					},
				}
				for _, name := range []string{"api-gateway", "booking", "check-in", "hotels", "review", "notification"} {
					cfg.Services = append(cfg.Services, config.Service{Name: name})
				}
				err = BusinessSmoke(ctx, cfg, notificationLogStub{bookingID: bookingID}, io.Discard)
				id = bookingID
			} else {
				id, err = scenario.run(ctx)
			}
			if test.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if test.wantError != "" && (err == nil || !strings.Contains(err.Error(), test.wantError)) {
				t.Fatalf("ожидали %q, получено %v", test.wantError, err)
			}
			if id != bookingID || !canceled || (test.fault == "" && !activated) {
				t.Fatalf("id=%s, отмена=%t, активация=%t", id, canceled, activated)
			}
		})
	}
}

func TestNotificationDelivered(t *testing.T) {
	for _, test := range []struct {
		name      string
		count     int
		duplicate bool
		sharedID  bool
		invalidID bool
		wantReady bool
		wantError bool
	}{
		{
			name:      "все три события",
			count:     3,
			wantReady: true,
		},
		{
			name:  "частичная доставка",
			count: 2,
		},
		{
			name:      "повтор допустим без inbox",
			count:     3,
			duplicate: true,
			wantReady: true,
		},
		{
			name:      "разные события не могут иметь общий ключ",
			count:     3,
			sharedID:  true,
			wantError: true,
		},
		{
			name:      "невалидный correlation_id",
			count:     3,
			invalidID: true,
			wantError: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			logs.WriteString("Pod: notification-123\n{\"msg\":\"service started\"}\n")
			bookingID := uuid.Must(uuid.NewV4()).String()
			correlationID := uuid.Must(uuid.NewV4()).String()
			for i, title := range []string{"Бронирование подтверждено", "Добро пожаловать!", "Бронирование отменено"} {
				if i >= test.count {
					break
				}
				if !test.sharedID {
					correlationID = uuid.Must(uuid.NewV4()).String()
				}
				if test.invalidID {
					correlationID = "broken"
				}
				entry := map[string]string{
					"booking_id":     bookingID,
					"correlation_id": correlationID,
					"provider":       "stdout",
					"channel":        "push",
					"msg":            title,
					"text":           "Текст для гостя",
				}
				json.NewEncoder(&logs).Encode(entry)
				if test.duplicate {
					json.NewEncoder(&logs).Encode(entry)
				}
			}
			ready, err := notificationDelivered(&logs, bookingID)
			if ready != test.wantReady || (err != nil) != test.wantError {
				t.Fatalf("готово=%t, ошибка=%v", ready, err)
			}
		})
	}
}

func TestBusinessWaitRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := awaitBusiness(ctx, func() (bool, error) {
		return false, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ожидали отмену контекста, получено %v", err)
	}
}

type notificationLogStub struct {
	bookingID string
}

func (s notificationLogStub) Logs(_ context.Context, service config.Service, follow bool, tail int64, out io.Writer) error {
	if service.Name != "notification" || follow || tail <= 0 {
		return errors.New("некорректные параметры чтения логов Kubernetes")
	}
	for _, title := range []string{"Бронирование подтверждено", "Добро пожаловать!", "Бронирование отменено"} {
		if err := json.NewEncoder(out).Encode(map[string]string{
			"booking_id":     s.bookingID,
			"correlation_id": uuid.Must(uuid.NewV4()).String(),
			"provider":       "stdout",
			"channel":        "push",
			"msg":            title,
			"text":           "Текст для гостя",
		}); err != nil {
			return err
		}
	}
	return nil
}
