package fault

import (
	"io"
	"testing"
	"testing/synctest"
	"time"
)

func TestHandlerUnavailableRuntime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		o := Options{From: "hotels", To: "review", Direction: "handler", Unavailable: true, Duration: time.Second}
		if err := o.Validate(); err != nil {
			t.Fatal(err)
		}
		values := o.Values()
		if values["demo.hotels.review.handler.abort_percent"] != "100" || values["demo.hotels.review.request.abort_percent"] != "0" {
			t.Fatal(values)
		}
		r := &fakeRuntime{values: map[string]string{"demo.handler_delay_supported": "1"}}
		if RunRuntime(t.Context(), r, o, io.Discard) == nil {
			t.Fatal("accepted old proxy")
		}
		r.values["demo.handler_abort_supported"] = "1"
		if err := RunRuntime(t.Context(), r, o, io.Discard); err != nil {
			t.Fatal(err)
		}
		if r.values["demo.hotels.review.handler.abort_percent"] != "0" {
			t.Fatal("fault not cleared")
		}
		pushback := int32(100)
		o.PushbackMS = &pushback
		if o.Validate() == nil {
			t.Fatal("accepted handler pushback")
		}
	})
}
