package notify

import (
	"strings"
	"testing"
)

func TestErrSentinels(t *testing.T) {
	if ErrUnsupported == nil {
		t.Fatal("ErrUnsupported must be non-nil")
	}
	if ErrUnavailable == nil {
		t.Fatal("ErrUnavailable must be non-nil")
	}
}

func TestUrgencyConstants(t *testing.T) {
	if UrgencyLow >= UrgencyNormal || UrgencyNormal >= UrgencyCritical {
		t.Fatalf("Urgency constants must be ordered low < normal < critical, got %d < %d < %d", UrgencyLow, UrgencyNormal, UrgencyCritical)
	}
	if UrgencyLow == 0 || UrgencyNormal == 0 || UrgencyCritical == 0 {
		t.Fatal("Urgency constants must be non-zero so the zero value can mean 'unset'")
	}
	for u, want := range map[Urgency]int{
		UrgencyLow:      0,
		UrgencyNormal:   1,
		UrgencyCritical: 2,
	} {
		if got := u.level(); got != want {
			t.Errorf("Urgency(%d).level() = %d, want %d", u, got, want)
		}
	}
}

func TestZeroValueOptionsIsNormalUrgency(t *testing.T) {
	if got := (Options{}).Urgency.level(); got != 1 {
		t.Errorf("zero Options urgency level = %d, want 1 (normal)", got)
	}
	if got := Urgency(0).level(); got != 1 {
		t.Errorf("Urgency(0).level() = %d, want 1 (normal)", got)
	}
	if got := Urgency(99).level(); got != 1 {
		t.Errorf("unknown Urgency(99).level() = %d, want 1 (normal)", got)
	}
}

func TestBeepDefaults(t *testing.T) {
	if DefaultFreq <= 0 {
		t.Errorf("DefaultFreq = %v, want > 0", DefaultFreq)
	}
	if DefaultDuration <= 0 {
		t.Errorf("DefaultDuration = %d, want > 0", DefaultDuration)
	}
}

func TestOptionsValidation(t *testing.T) {
	ok := []Options{
		{},
		{Icon: "icon.png"},
		{IconData: []byte("png-bytes")},
		{Urgency: UrgencyCritical},
		{Icon: "i.png", Urgency: UrgencyLow},
	}
	for _, o := range ok {
		if err := o.validate(); err != nil {
			t.Errorf("Options%+v.validate() = %v, want nil", o, err)
		}
	}
	bad := []Options{
		{Icon: "icon.png", IconData: []byte("png-bytes")},
		{IconData: []byte{}},
	}
	for _, o := range bad {
		if err := o.validate(); err == nil {
			t.Errorf("Options%+v.validate() = nil, want error", o)
		}
	}
	if err := (Options{Icon: "i.png", IconData: []byte("p")}).validate(); !strings.Contains(err.Error(), "at most one") {
		t.Errorf("conflict error = %v, want mention of 'at most one'", err)
	}
	if err := (Options{IconData: []byte{}}).validate(); !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty IconData error = %v, want mention of 'empty'", err)
	}
}

func TestShowOptsRejectsEarly(t *testing.T) {
	if err := ShowOpts("test", "t", "m", Options{Icon: "x.png", IconData: []byte("p")}); err == nil {
		t.Fatal("ShowOpts with Icon and IconData both set must fail before reaching the platform")
	}
	if err := ShowOpts("test", "t", "m", Options{IconData: []byte{}}); err == nil {
		t.Fatal("ShowOpts with empty IconData must fail before reaching the platform")
	}
	if err := Alert("test", "t", "m", Options{Icon: "x.png", IconData: []byte("p")}); err == nil {
		t.Fatal("Alert with Icon and IconData both set must fail before reaching the platform")
	}
}
