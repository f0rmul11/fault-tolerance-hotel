package checkin

import (
	"context"
	"strconv"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"check-in/internal/application/usecase/checkin/arrivals"
	"check-in/internal/domain/entity"
	"check-in/internal/domain/vo"
	pb "check-in/internal/pkg/pb/check-in/checkin/v1"
	"check-in/internal/pkg/telemetry/traces"
)

func (s *Server) ListArrivals(ctx context.Context, r *pb.ListArrivalsRequest) (*pb.ListArrivalsResponse, error) {
	ctx, span := traces.Start(ctx, "handler.checkin.ListArrivals")
	defer span.End()

	period := mo.None[vo.Period]()
	if r.GetStartsAt() != "" || r.GetEndsAt() != "" {
		value, err := vo.ParsePeriod(r.GetStartsAt(), r.GetEndsAt())
		if err != nil {
			return nil, err
		}
		period = mo.Some(value)
	}
	checkins, err := s.usecases.ListArrivals.Execute(ctx, arrivals.Input{Period: period})
	if err != nil {
		return nil, err
	}
	return &pb.ListArrivalsResponse{Checkins: lo.Map(checkins, func(c *entity.CheckIn, _ int) *pb.CheckInView {
		return &pb.CheckInView{
			Id:         c.ID().String(),
			BookingId:  c.BookingID().String(),
			RoomId:     strconv.FormatInt(c.RoomID(), 10),
			RoomNumber: c.RoomNumber(),
			RoomType:   c.RoomType(),
			GuestId:    c.GuestID().String(),
			StartsAt:   c.Period().Start().Format(time.RFC3339Nano),
			EndsAt:     c.Period().End().Format(time.RFC3339Nano),
			Status:     string(c.Status()),
			Version:    int32(c.Version()),
			Failure:    c.Failure(),
		}
	})}, nil
}
