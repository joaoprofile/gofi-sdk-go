package core

import (
	"testing"
	"time"

	"github.com/gofi-labs/gofi-sdk-go/base/environment"
)

func TestSetTimezone(t *testing.T) {
	orig := time.Local
	t.Cleanup(func() { time.Local = orig })

	if got := Timezone(&environment.Environment{Timezone: "America/Sao_Paulo"}); got.Name != "America/Sao_Paulo" {
		t.Fatalf("Timezone=%+v", got)
	}
	if err := SetTimezone(&environment.Environment{Timezone: "America/Sao_Paulo"}); err != nil {
		t.Fatalf("SetTimezone: %v", err)
	}
	if time.Local.String() != "America/Sao_Paulo" {
		t.Errorf("time.Local=%s", time.Local)
	}
	if err := SetTimezone(&environment.Environment{Timezone: "Not/AZone"}); err == nil {
		t.Error("an invalid TIMEZONE must fail")
	}
}
