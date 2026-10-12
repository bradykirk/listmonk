package brands

import "testing"

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
