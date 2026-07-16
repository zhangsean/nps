package file

import (
	"reflect"
	"testing"
)

func TestGetRoundRobinTargets(t *testing.T) {
	target := &Target{TargetStr: "127.0.0.1:8080\n127.0.0.1:8081\n127.0.0.1:8082"}

	first, err := target.GetRoundRobinTargets()
	if err != nil {
		t.Fatalf("GetRoundRobinTargets returned error: %v", err)
	}
	if want := []string{"127.0.0.1:8081", "127.0.0.1:8082", "127.0.0.1:8080"}; !reflect.DeepEqual(first, want) {
		t.Fatalf("unexpected first target order, want %v, got %v", want, first)
	}

	second, err := target.GetRoundRobinTargets()
	if err != nil {
		t.Fatalf("GetRoundRobinTargets returned error: %v", err)
	}
	if want := []string{"127.0.0.1:8082", "127.0.0.1:8080", "127.0.0.1:8081"}; !reflect.DeepEqual(second, want) {
		t.Fatalf("unexpected second target order, want %v, got %v", want, second)
	}
}

func TestGetRoundRobinTargetsTrimsEmptyTargets(t *testing.T) {
	target := &Target{TargetStr: " 127.0.0.1:8080 \n\n127.0.0.1:8081\n"}

	targets, err := target.GetRoundRobinTargets()
	if err != nil {
		t.Fatalf("GetRoundRobinTargets returned error: %v", err)
	}
	if want := []string{"127.0.0.1:8081", "127.0.0.1:8080"}; !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected target order, want %v, got %v", want, targets)
	}
}
