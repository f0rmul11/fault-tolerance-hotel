package hotels

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"

	"hotels/internal/application/service/hotel/enrich"
	"hotels/internal/application/usecase"
	"hotels/internal/application/usecase/hotel/list"
	reviewgateway "hotels/internal/infrastructure/gateway/review"
	hotelstorage "hotels/internal/infrastructure/storage/hotel"
	reviewpb "hotels/internal/pkg/pb/external/review/review/v1"
	pb "hotels/internal/pkg/pb/hotels/hotels/v1"
	"hotels/internal/pkg/transaction/wrapper"
)

type traceContextKey struct{}

// Real handler, use case, service, gateway and storage execute together. Only
// SQL execution and the external RPC are fake, so this checks propagation through
// the actual architecture without a running database or Review process.
func TestListHotelsLayerTrace(t *testing.T) {
	queryFailure := errors.New("query unavailable")
	tests := []struct {
		name       string
		queryError error
		wantSpans  int
	}{
		{
			name:      "complete layer tree",
			wantSpans: 5,
		},
		{
			name:       "storage failure ends active spans and stops enrichment",
			queryError: queryFailure,
			wantSpans:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(provider)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Errorf("shutdown tracer provider: %v", err)
				}
			})

			hotelID := uuid.FromStringOrNil("80575445-d08c-49a3-8ef8-6d9180586941")
			executor := &traceExecutor{
				hotelID: hotelID,
				err:     tt.queryError,
			}
			rpc := &traceReviewClient{}
			service := New(&usecase.Registry{
				ListHotels: list.New(
					hotelstorage.New(traceDatabase{executor: executor}),
					enrich.New(reviewgateway.NewClient(rpc)),
				),
			})

			incoming := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    trace.TraceID{1},
				SpanID:     trace.SpanID{2},
				TraceFlags: trace.FlagsSampled,
				Remote:     true,
			})
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), traceContextKey{}, "request-value"))
			t.Cleanup(cancel)
			ctx = trace.ContextWithRemoteSpanContext(ctx, incoming)

			response, err := service.ListHotels(ctx, &pb.ListHotelsRequest{})
			if !errors.Is(err, tt.queryError) {
				t.Fatalf("error = %v, want %v", err, tt.queryError)
			}
			if tt.queryError == nil {
				if len(response.GetHotels()) != 1 || response.GetHotels()[0].GetId() != hotelID.String() {
					t.Fatalf("unexpected response: %v", response)
				}
				if rpc.ctx == nil || len(rpc.ids) != 1 || rpc.ids[0] != hotelID.String() {
					t.Fatal("enrichment must call Review for the returned hotel")
				}
			} else if rpc.ctx != nil {
				t.Fatal("Review must not be called after storage failure")
			}

			ended := recorder.Ended()
			if len(ended) != tt.wantSpans {
				t.Fatalf("ended spans = %d, want %d", len(ended), tt.wantSpans)
			}
			byName := make(map[string]sdktrace.ReadOnlySpan, len(ended))
			for _, span := range ended {
				byName[span.Name()] = span
				if span.SpanContext().TraceID() != incoming.TraceID() || span.EndTime().IsZero() {
					t.Fatalf("span %q lost the incoming trace or was not ended", span.Name())
				}
			}

			parents := map[string]string{
				"handler.hotels.ListHotels": "",
				"usecase.hotel.list":        "handler.hotels.ListHotels",
				"storage.hotel.List":        "usecase.hotel.list",
			}
			if tt.queryError == nil {
				parents["service.hotel.enrich.Hotels"] = "usecase.hotel.list"
				parents["gateway.review.GetHotelRatings"] = "service.hotel.enrich.Hotels"
			}
			for name, parentName := range parents {
				span, ok := byName[name]
				if !ok {
					t.Fatalf("missing span %q", name)
				}
				parentID := incoming.SpanID()
				if parentName != "" {
					parent, ok := byName[parentName]
					if !ok {
						t.Fatalf("missing parent %q", parentName)
					}
					parentID = parent.SpanContext().SpanID()
				}
				if span.Parent().SpanID() != parentID {
					t.Errorf("span %q parent = %s, want %s", name, span.Parent().SpanID(), parentID)
				}
			}

			boundaries := map[string]context.Context{
				"storage.hotel.List": executor.ctx,
			}
			if rpc.ctx != nil {
				boundaries["gateway.review.GetHotelRatings"] = rpc.ctx
			}
			cancel()
			for name, boundaryCtx := range boundaries {
				if boundaryCtx == nil || boundaryCtx.Value(traceContextKey{}) != "request-value" || !errors.Is(boundaryCtx.Err(), context.Canceled) {
					t.Fatalf("%q lost context values or cancellation", name)
				}
				if trace.SpanContextFromContext(boundaryCtx).SpanID() != byName[name].SpanContext().SpanID() {
					t.Errorf("%q did not pass its child context to the boundary", name)
				}
			}
		})
	}
}

type traceDatabase struct {
	executor *traceExecutor
}

func (d traceDatabase) Pool(context.Context) wrapper.Executor {
	return d.executor
}

type traceExecutor struct {
	wrapper.Executor
	hotelID uuid.UUID
	err     error
	ctx     context.Context
}

func (e *traceExecutor) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	e.ctx = ctx
	if e.err != nil {
		return nil, e.err
	}
	return &traceRows{hotelID: e.hotelID}, nil
}

type traceRows struct {
	pgx.Rows
	hotelID uuid.UUID
	read    bool
}

func (r *traceRows) FieldDescriptions() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{
		{Name: "id"},
	}
}

func (r *traceRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *traceRows) Scan(dest ...any) error {
	*dest[0].(*uuid.UUID) = r.hotelID
	return nil
}

func (r *traceRows) Err() error {
	return nil
}

func (r *traceRows) Close() {
}

type traceReviewClient struct {
	ctx context.Context
	ids []string
}

func (c *traceReviewClient) GetHotelRatings(ctx context.Context, request *reviewpb.GetHotelRatingsRequest, _ ...grpc.CallOption) (*reviewpb.GetHotelRatingsResponse, error) {
	c.ctx = ctx
	c.ids = request.GetHotelIds()
	return &reviewpb.GetHotelRatingsResponse{}, nil
}
