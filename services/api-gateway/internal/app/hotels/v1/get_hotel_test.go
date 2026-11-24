package hotels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"api-gateway/internal/pkg/httptransport"
	pb "api-gateway/internal/pkg/pb/api-gateway/hotels/v1"
	external "api-gateway/internal/pkg/pb/external/hotels/hotels/v1"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

type versionHotelsClient struct {
	external.HotelsServiceClient
	request *external.GetHotelRequest
	err     error
}

func (c *versionHotelsClient) GetHotel(_ context.Context, request *external.GetHotelRequest, _ ...grpc.CallOption) (*external.GetHotelResponse, error) {
	c.request = request
	return &external.GetHotelResponse{Hotel: &external.Hotel{Id: request.GetHotelId(), RatingVersion: 43}}, c.err
}

func TestGetHotelMinimumVersionHTTP(t *testing.T) {
	const id = "1b085786-a2db-43aa-8387-4e67cd626647"
	for _, tt := range []struct {
		name    string
		query   string
		minimum int64
		err     error
		status  int
	}{
		{name: "default", status: http.StatusOK},
		{name: "minimum forwarded", query: "?min_rating_version=42", minimum: 42, status: http.StatusOK},
		{name: "unavailable preserved", query: "?min_rating_version=42", minimum: 42, err: status.Error(codes.Unavailable, "Запрошенная версия рейтинга пока недоступна"), status: http.StatusServiceUnavailable},
		{name: "owner validates minimum", query: "?min_rating_version=-1", minimum: -1, err: status.Error(codes.InvalidArgument, "Минимальная версия рейтинга не может быть отрицательной"), status: http.StatusUnprocessableEntity},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := &versionHotelsClient{err: tt.err}
			desc := pb.NewHotelsServiceServiceDesc(NewHotelsService(client))
			mux := runtime.NewServeMux(httptransport.Options()...)
			if err := desc.RegisterHTTP(t.Context(), mux); err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/hotels/"+id+tt.query, nil))
			if response.Code != tt.status {
				t.Fatalf("status=%d, body=%s", response.Code, response.Body.String())
			}
			if client.request == nil || client.request.GetHotelId() != id || client.request.GetMinRatingVersion() != tt.minimum {
				t.Fatalf("request not forwarded: %v", client.request)
			}
			if tt.err == nil {
				var body pb.GetHotelResponse
				if err := protojson.Unmarshal(response.Body.Bytes(), &body); err != nil || body.GetHotel().GetRatingVersion() != 43 {
					t.Fatalf("response version lost: %s, %v", response.Body.String(), err)
				}
			}
		})
	}
}
