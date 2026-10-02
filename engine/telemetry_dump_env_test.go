package engine

import "testing"

func TestTelemetryDumpPathPrefersUrdVariable(t *testing.T) {
	t.Setenv(telemetryDumpEnv, "/new")
	t.Setenv(legacyTelemetryDumpEnv, "/old")
	path, legacy := telemetryDumpPath()
	if path != "/new" || legacy {
		t.Errorf("telemetryDumpPath() = %q, %v; want /new, false", path, legacy)
	}
}

func TestTelemetryDumpPathFallsBackToEgoVariable(t *testing.T) {
	t.Setenv(telemetryDumpEnv, "")
	t.Setenv(legacyTelemetryDumpEnv, "/old")
	path, legacy := telemetryDumpPath()
	if path != "/old" || !legacy {
		t.Errorf("telemetryDumpPath() = %q, %v; want /old, true", path, legacy)
	}
}
