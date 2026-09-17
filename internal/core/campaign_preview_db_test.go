package core

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/knadh/listmonk/models"
	"github.com/lib/pq"
	null "gopkg.in/volatiletech/null.v6"
)

// TestCampaignPreviewTextQueries checks that create-campaign and
// update-campaign persist preview_text, and that SELECT campaigns.* maps it
// onto models.Campaign (see TestDashboardGrowthSQL for LISTMONK_TEST_DSN).
func TestCampaignPreviewTextQueries(t *testing.T) {
	dsn := os.Getenv("LISTMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("LISTMONK_TEST_DSN not set")
	}

	db, q := newTestDB(t, dsn, "lm_preview_test", "")

	var id int
	if err := q.CreateCampaign.Get(&id,
		uuid.Must(uuid.NewV4()), models.CampaignTypeRegular, "Preview test", "Subject", "from@example.com",
		"<p>Body</p>", null.String{}, models.CampaignContentTypeHTML, null.Time{},
		models.Headers{}, models.JSON{}, pq.StringArray{}, "email", null.Int{},
		pq.Array([]int{}), false, null.String{}, null.Int{}, json.RawMessage("{}"),
		pq.Array([]int{}), null.String{},
		"First look inside"); err != nil {
		t.Fatalf("create-campaign: %v", err)
	}

	read := func() string {
		t.Helper()
		var c models.Campaign
		if err := db.Unsafe().Get(&c, `SELECT campaigns.* FROM campaigns WHERE id = $1`, id); err != nil {
			t.Fatalf("read campaign: %v", err)
		}
		return c.PreviewText
	}

	if got := read(); got != "First look inside" {
		t.Errorf("after create: preview_text = %q", got)
	}

	if _, err := q.UpdateCampaign.Exec(id,
		"Preview test", "Subject", "from@example.com", "<p>Body</p>", null.String{},
		models.CampaignContentTypeHTML, null.Time{}, models.Headers{}, models.JSON{}, pq.StringArray{},
		"email", null.Int{}, pq.Array([]int{}), false, null.String{}, null.Int{}, json.RawMessage("{}"),
		pq.Array([]int{}), null.String{},
		"Updated preview"); err != nil {
		t.Fatalf("update-campaign: %v", err)
	}

	if got := read(); got != "Updated preview" {
		t.Errorf("after update: preview_text = %q", got)
	}
}
