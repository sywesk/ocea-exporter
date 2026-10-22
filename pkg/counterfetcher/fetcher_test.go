package counterfetcher

import (
	"reflect"
	"testing"

	"github.com/sywesk/ocea-exporter/pkg/oceaapi"
)

func TestMergeDevices(t *testing.T) {
	cold := oceaapi.Device{AppareilID: "cold", ValeurIndex: 20}
	oldCold := oceaapi.Device{AppareilID: "cold", ValeurIndex: 19}
	heat := oceaapi.Device{AppareilID: "heat", ValeurIndex: 500}
	hot := oceaapi.Device{AppareilID: "hot", ValeurIndex: 10}
	for _, tc := range []struct {
		name                   string
		today, yesterday, want []oceaapi.Device
	}{
		{"no readings today", nil, []oceaapi.Device{cold, heat, hot}, []oceaapi.Device{cold, heat, hot}},
		{"partial readings", []oceaapi.Device{cold}, []oceaapi.Device{oldCold, heat, hot}, []oceaapi.Device{cold, heat, hot}},
		{"new meter today", []oceaapi.Device{cold, hot}, []oceaapi.Device{oldCold, heat}, []oceaapi.Device{cold, hot, heat}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mergeDevices(tc.today, tc.yesterday); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
	if containsDevices([]oceaapi.Device{cold, hot}, []oceaapi.Device{cold, heat}) {
		t.Fatal("equal counts must not hide a missing meter")
	}
}
