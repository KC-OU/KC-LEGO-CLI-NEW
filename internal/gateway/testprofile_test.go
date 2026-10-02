package gateway

import (
	"reflect"
	"testing"
)

func TestMergeEnvOverridesByKeyKeepsBaseOrderOtherwise(t *testing.T) {
	base := []string{"TERM=dumb", "PATH=/usr/bin", "WMS_GATEWAY_SESSION=1"}
	overrides := []string{"PATH=/root/test/bin", "LEGO_DB_PATH=/root/test/lego.db"}

	got := mergeEnv(base, overrides)
	want := []string{"TERM=dumb", "PATH=/root/test/bin", "WMS_GATEWAY_SESSION=1", "LEGO_DB_PATH=/root/test/lego.db"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeEnv = %v, want %v", got, want)
	}
}

func TestMergeEnvDoesNotMutateBase(t *testing.T) {
	base := []string{"PATH=/usr/bin"}
	_ = mergeEnv(base, []string{"PATH=/other"})
	if base[0] != "PATH=/usr/bin" {
		t.Errorf("base must not be mutated in place, got %v", base)
	}
}

func TestMergeEnvNilOverridesIsAPlainCopy(t *testing.T) {
	base := []string{"A=1", "B=2"}
	got := mergeEnv(base, nil)
	if !reflect.DeepEqual(got, base) {
		t.Fatalf("mergeEnv(base, nil) = %v, want %v unchanged", got, base)
	}
}
