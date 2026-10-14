package hotels

import (
	"context"
	"strconv"
	"time"

	"github.com/gofrs/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"booking/internal/domain/vo"
	roomdto "booking/internal/dto/room"
	"booking/internal/pkg/grpc/intercept"
	pb "booking/internal/pkg/pb/external/hotels/hotels/v1"
)

type ExternalClient pb.HotelsServiceClient

func NewExternalClient(conn grpc.ClientConnInterface) ExternalClient {
	return pb.NewHotelsServiceClient(conn)
}

type Client struct {
	external ExternalClient
}

func NewClient(external ExternalClient) *Client {
	return &Client{external: external}
}

func (c *Client) Get(ctx context.Context, id int64) (roomdto.Room, error) {
	var trailers metadata.MD
	response, err := c.external.GetRoom(ctx, &pb.GetRoomRequest{RoomId: strconv.FormatInt(id, 10)}, grpc.Trailer(&trailers))
	if err != nil {
		return roomdto.Room{}, intercept.FromRPC(err, trailers)
	}
	return roomdto.Room{
		ID:       id,
		Number:   response.GetRoom().GetNumber(),
		RoomType: response.GetRoom().GetRoomType(),
		Limits: vo.Limits{
			Min: time.Duration(response.GetRoom().GetMinDurationSeconds()) * time.Second,
			Max: time.Duration(response.GetRoom().GetMaxDurationSeconds()) * time.Second,
		},
	}, nil
}

func (c *Client) List(ctx context.Context, hotelID uuid.UUID) ([]roomdto.Room, error) {
	var trailers metadata.MD
	response, err := c.external.ListRooms(ctx, &pb.ListRoomsRequest{HotelId: hotelID.String()}, grpc.Trailer(&trailers))
	if err != nil {
		return nil, intercept.FromRPC(err, trailers)
	}

	rooms := make([]roomdto.Room, 0, len(response.GetRooms()))
	for _, room := range response.GetRooms() {
		id, err := strconv.ParseInt(room.GetId(), 10, 64)
		if err != nil {
			return nil, err
		}

		rooms = append(rooms, roomdto.Room{
			ID:       id,
			Number:   room.GetNumber(),
			RoomType: room.GetRoomType(),
			Limits: vo.Limits{
				Min: time.Duration(room.GetMinDurationSeconds()) * time.Second,
				Max: time.Duration(room.GetMaxDurationSeconds()) * time.Second,
			},
		})
	}
	return rooms, nil
}
