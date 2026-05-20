package scan

import "testing"

const sonarSample = `[
  {
    "port": 41548,
    "pid": 20703,
    "process": "node",
    "bind_address": "127.0.0.1",
    "ip_version": "IPv4",
    "type": "user"
  },
  {
    "port": 9527,
    "pid": 1106083,
    "process": "MainThread",
    "bind_address": "0.0.0.0",
    "ip_version": "IPv4",
    "type": "user"
  },
  {
    "port": 22,
    "pid": 1,
    "process": "sshd",
    "bind_address": "0.0.0.0",
    "ip_version": "IPv4",
    "type": "system"
  }
]`

func TestParseSonarJSON_happy(t *testing.T) {
	ports, err := ParseSonarJSON([]byte(sonarSample))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ports) != 3 {
		t.Fatalf("got %d ports, want 3", len(ports))
	}
	if ports[0].Port != 41548 || ports[0].Process != "node" || ports[0].PID != 20703 {
		t.Errorf("ports[0] = %#v", ports[0])
	}
	if ports[2].Type != "system" {
		t.Errorf("ports[2].Type = %q, want system", ports[2].Type)
	}
}

func TestParseSonarJSON_empty(t *testing.T) {
	ports, err := ParseSonarJSON([]byte("[]"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(ports) != 0 {
		t.Errorf("got %d ports, want 0", len(ports))
	}
}

func TestParseSonarJSON_malformed(t *testing.T) {
	if _, err := ParseSonarJSON([]byte("not json")); err == nil {
		t.Errorf("want error for malformed json")
	}
}
