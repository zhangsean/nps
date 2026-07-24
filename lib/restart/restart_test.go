package restart

import (
	"reflect"
	"testing"
)

func TestPayloadRoundTrip(t *testing.T) {
	want := helperPayload{
		ParentPID:   123,
		Executable:  `C:\nps\nps.exe`,
		Arguments:   []string{"-conf_path=C:\\nps", "service"},
		WorkingDir:  `C:\nps`,
		ServiceMode: true,
	}
	encoded, err := encodePayload(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodePayload(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = %#v, want %#v", got, want)
	}
}

func TestServiceRestartArgumentsPreserveOnlyConfigPath(t *testing.T) {
	got := serviceRestartArguments([]string{"service", "-conf_path=C:\\nps", "-version", "unexpected"})
	want := []string{"restart", "-conf_path=C:\\nps"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
}
