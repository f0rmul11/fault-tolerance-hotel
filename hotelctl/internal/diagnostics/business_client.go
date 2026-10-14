package diagnostics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
)

// Модели публичного HTTP API, а не внутренние сущности сервисов.
type scenarioBooking struct {
	ID         string `json:"id"`
	RoomID     string `json:"room_id"`
	RoomNumber string `json:"room_number"`
	RoomType   string `json:"room_type"`
	Status     string `json:"status"`
	Version    int    `json:"version"`
}

type bookingResponse struct {
	Booking scenarioBooking `json:"booking"`
}

type scenarioCheckIn struct {
	ID        string `json:"id"`
	BookingID string `json:"booking_id"`
	Status    string `json:"status"`
	Failure   string `json:"failure"`
}

type checkInResponse struct {
	CheckIn scenarioCheckIn `json:"checkin"`
}

type scenarioRoom struct {
	ID          string      `json:"id"`
	HotelID     string      `json:"hotel_id"`
	Number      string      `json:"number"`
	RoomType    string      `json:"room_type"`
	MinDuration json.Number `json:"min_duration_seconds"`
	MaxDuration json.Number `json:"max_duration_seconds"`
}

type createBookingRequest struct {
	RoomID         string `json:"room_id"`
	IdempotencyKey string `json:"idempotency_key"`
	StartsAt       string `json:"starts_at"`
	EndsAt         string `json:"ends_at"`
}

type businessScenario struct {
	base   string
	client *http.Client
	actor  string
	out    io.Writer
}

func (s *businessScenario) request(ctx context.Context, method, path, role string, input, result any, expected int) error {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			return err
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, s.base+path, &body)
	if err != nil {
		return err
	}
	requestID, err := uuid.NewV4()
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Demo-Role", role)
	request.Header.Set("X-Demo-Actor", s.actor)
	request.Header.Set("X-Request-Id", requestID.String())
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode != expected {
		return fmt.Errorf("%s %s: HTTP %d, ожидался %d: %s", method, path, response.StatusCode, expected, data)
	}
	if result != nil {
		if err := json.Unmarshal(data, result); err != nil {
			return fmt.Errorf("%s %s: некорректный ответ: %w", method, path, err)
		}
	}
	return nil
}

func (s *businessScenario) freeRoom(ctx context.Context) (scenarioRoom, createBookingRequest, error) {
	var catalog struct {
		Hotels []struct {
			ID string `json:"id"`
		} `json:"hotels"`
	}
	if err := s.request(ctx, http.MethodGet, "/v1/hotels", "GUEST", nil, &catalog, http.StatusOK); err != nil {
		return scenarioRoom{}, createBookingRequest{}, err
	}
	start := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	for _, hotel := range catalog.Hotels {
		var rooms struct {
			Rooms []scenarioRoom `json:"rooms"`
		}
		if err := s.request(ctx, http.MethodGet, "/v1/hotels/"+hotel.ID+"/rooms", "GUEST", nil, &rooms, http.StatusOK); err != nil {
			return scenarioRoom{}, createBookingRequest{}, err
		}
		for _, room := range rooms.Rooms {
			minimum, err := room.MinDuration.Int64()
			if err != nil {
				return scenarioRoom{}, createBookingRequest{}, fmt.Errorf("длительность номера %s: %w", room.ID, err)
			}
			var maximum int64
			if room.MaxDuration != "" {
				maximum, err = room.MaxDuration.Int64()
				if err != nil {
					return scenarioRoom{}, createBookingRequest{}, err
				}
			}
			duration := max(12*int64(time.Hour/time.Second), minimum)
			if maximum > 0 && duration > maximum {
				continue
			}
			key, err := uuid.NewV4()
			if err != nil {
				return scenarioRoom{}, createBookingRequest{}, err
			}
			input := createBookingRequest{
				RoomID:         room.ID,
				IdempotencyKey: key.String(),
				StartsAt:       start.Format(time.RFC3339),
				EndsAt:         start.Add(time.Duration(duration) * time.Second).Format(time.RFC3339),
			}
			available, err := s.available(ctx, room, input)
			if err != nil {
				return scenarioRoom{}, createBookingRequest{}, err
			}
			if available {
				return room, input, nil
			}
		}
	}
	return scenarioRoom{}, createBookingRequest{}, fmt.Errorf("нет свободного номера для проверки; освободите номер в учебном стенде")
}

func (s *businessScenario) available(ctx context.Context, room scenarioRoom, input createBookingRequest) (bool, error) {
	var result struct {
		RoomIDs []string `json:"room_ids"`
	}
	err := s.request(ctx, http.MethodPost, "/v1/rooms/availability", "GUEST", map[string]string{
		"hotel_id":  room.HotelID,
		"starts_at": input.StartsAt,
		"ends_at":   input.EndsAt,
	}, &result, http.StatusOK)
	if err != nil {
		return false, err
	}
	for _, id := range result.RoomIDs {
		if id == room.ID {
			return true, nil
		}
	}
	return false, nil
}
