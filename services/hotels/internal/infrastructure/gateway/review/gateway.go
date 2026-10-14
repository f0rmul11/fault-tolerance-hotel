package review

import (
	"context"

	"github.com/gofrs/uuid"
	"github.com/samber/lo"
	"google.golang.org/grpc"

	dto "hotels/internal/dto/hotel"
	"hotels/internal/pkg/grpc/intercept"
	pb "hotels/internal/pkg/pb/external/review/review/v1"
)

type ExternalClient pb.ReviewServiceClient

func NewExternalClient(conn grpc.ClientConnInterface) ExternalClient {
	return pb.NewReviewServiceClient(conn)
}

type Client struct {
	external ExternalClient
}

func NewClient(external ExternalClient) *Client {
	return &Client{external: external}
}

func (c *Client) GetHotelRatings(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]dto.Rating, error) {
	response, err := c.external.GetHotelRatings(ctx, &pb.GetHotelRatingsRequest{
		HotelIds: lo.Map(ids, func(id uuid.UUID, _ int) string {
			return id.String()
		})},
	)
	if err != nil {
		return nil, intercept.FromRPC(err, nil)
	}
	result := make(map[uuid.UUID]dto.Rating, len(response.GetRatings()))
	for _, rating := range response.GetRatings() {
		result[uuid.FromStringOrNil(rating.GetHotelId())] = dto.Rating{
			Value:       rating.GetRating(),
			ReviewCount: rating.GetReviewCount(),
		}
	}
	return result, nil
}
