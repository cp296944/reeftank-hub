package dosing

import "context"

type Transport interface {
	Read(context.Context) (State, error)
	WriteHead(context.Context, Head) error
	Dose(context.Context, int, float64) (float64, error)
	Prime(context.Context, int, bool) error
	SyncTime(context.Context) error
}
type Capability struct {
	Name        string `json:"name"`
	Software    bool   `json:"software"`
	BLEVerified bool   `json:"ble_verified"`
	Evidence    string `json:"evidence"`
}

func Capabilities() []Capability {
	return []Capability{{"four_heads", true, false, "APK DosingChannel"}, {"manual_dose", true, false, "APK Manual dosing/tempDosing"}, {"calibration", true, false, "APK DosingCalibrateWidget"}, {"priming", true, false, "APK Priming"}, {"daily_quantity", true, false, "APK DosingQuantityWidget"}, {"periods", true, false, "APK Dosing periods"}, {"delay_between_supplements", true, false, "APK 30-second interaction guard"}, {"missed_dose_compensation", true, false, "APK battery-dependent compensation"}}
}
