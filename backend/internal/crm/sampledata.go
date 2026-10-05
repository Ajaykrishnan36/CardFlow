package crm

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// SampleData puts a small set of example records into one business so its screens aren't
// empty: CRM_SAMPLE_DATA=<business code>, e.g. ajay-traders. It runs once per business
// (a marker in crm.connector_state); removing the variable afterwards changes nothing.
// Every record it makes says "Sample" in its description and can be deleted like any other.
func (m *Module) SampleData(ctx context.Context) {
	code := strings.TrimSpace(os.Getenv("CRM_SAMPLE_DATA"))
	if code == "" || m.store == nil || m.records == nil {
		return
	}
	made, err := m.records.SeedSample(ctx, code)
	if err != nil {
		slog.Error("crm: sample data not created — nothing was changed", "business", code, "error", err)
		return
	}
	if made > 0 {
		slog.Info("crm: sample data created", "business", code, "records", made)
	}
}
