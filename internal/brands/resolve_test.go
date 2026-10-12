package brands

import (
	"reflect"
	"testing"
)

var testBrands = []Brand{
	{Slug: "ri", ListTags: []string{"ri", "ri-cold"}},
	{Slug: "ta", ListTags: []string{"ta"}},
}

func TestBrandOfList(t *testing.T) {
	cases := []struct {
		tags []string
		want ListBrand
	}{
		{[]string{"ri"}, ListBrand{Slug: "ri"}},
		{[]string{"test", "ri"}, ListBrand{Slug: "ri"}},
		{[]string{"ri-cold"}, ListBrand{Slug: "ri"}},
		{[]string{"ta"}, ListBrand{Slug: "ta"}},
		{[]string{"ri", "ta"}, ListBrand{Ambiguous: true}},
		{[]string{"other"}, ListBrand{}},
		{nil, ListBrand{}},
	}
	for _, c := range cases {
		if got := BrandOfList(c.tags, testBrands); got != c.want {
			t.Errorf("%v: got %+v want %+v", c.tags, got, c.want)
		}
	}
}

func TestCampaignBrand(t *testing.T) {
	tags := map[int][]string{3: {"ri"}, 6: {"ri-cold"}, 17: {"ta"}, 20: {"x"}, 21: {"ri", "ta"}}
	if s, _ := CampaignBrand([]int{3, 6}, tags, testBrands); s != "ri" {
		t.Fatal(s)
	}
	if s, r := CampaignBrand([]int{3, 17}, tags, testBrands); s != "" || r == "" {
		t.Fatal("mixed accepted")
	}
	if s, r := CampaignBrand(nil, tags, testBrands); s != "" || r == "" {
		t.Fatal("no lists accepted")
	}
	if s, _ := CampaignBrand([]int{20}, tags, testBrands); s != "" {
		t.Fatal("unassigned accepted")
	}
	if s, _ := CampaignBrand([]int{21}, tags, testBrands); s != "" {
		t.Fatal("ambiguous accepted")
	}
	if s, _ := CampaignBrand([]int{0}, tags, testBrands); s != "" {
		t.Fatal("deleted list accepted")
	}
}

func TestBrandListIDs(t *testing.T) {
	tags := map[int][]string{3: {"ri"}, 6: {"ri-cold"}, 17: {"ta"}, 21: {"ri", "ta"}}
	got := BrandListIDs("ri", tags, testBrands)
	if len(got) != 2 || got[0] != 3 || got[1] != 6 {
		t.Fatal(got)
	}
}

func TestListsOutsideBrand(t *testing.T) {
	if got := ListsOutside([]int{2, 5}, []int{2, 3}); !reflect.DeepEqual(got, []int{5}) {
		t.Fatal(got)
	}
	if got := ListsOutside(nil, []int{2}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestStatusChanged(t *testing.T) {
	if StatusChanged("", "enabled") {
		t.Fatal("empty status is no change")
	}
	if StatusChanged("enabled", "enabled") {
		t.Fatal("same status is no change")
	}
	if !StatusChanged("blocklisted", "enabled") {
		t.Fatal("blocklist not detected")
	}
	if !StatusChanged("enabled", "blocklisted") {
		t.Fatal("un-blocklist not detected")
	}
}

func TestScopedPermittedListsNeverEmpty(t *testing.T) {
	if got := ScopedPermittedLists(nil); !reflect.DeepEqual(got, []int{-1}) {
		t.Fatal(got)
	}
	if got := ScopedPermittedLists([]int{}); !reflect.DeepEqual(got, []int{-1}) {
		t.Fatal(got)
	}
	if got := ScopedPermittedLists([]int{2}); !reflect.DeepEqual(got, []int{2}) {
		t.Fatal(got)
	}
}

func TestBrandListIDsEmptySlug(t *testing.T) {
	// Lists with no brand tag must not match an empty slug.
	tags := map[int][]string{3: {"ri"}, 9: {"unrelated"}, 10: nil}
	got := BrandListIDs("", tags, testBrands)
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want empty non-nil slice", got)
	}
}

func TestForceBrandTags(t *testing.T) {
	ri := testBrands[0]
	got, err := ForceBrandTags([]string{"promo", "ta", "ri-cold"}, ri, testBrands)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"promo", ri.ListTags[0]}) {
		t.Fatalf("got %v", got)
	}
	if got, _ := ForceBrandTags(nil, ri, testBrands); !reflect.DeepEqual(got, []string{ri.ListTags[0]}) {
		t.Fatalf("got %v", got)
	}
	if _, err := ForceBrandTags(nil, Brand{Slug: "none"}, testBrands); err == nil {
		t.Fatal("brand without list tags accepted")
	}
}
