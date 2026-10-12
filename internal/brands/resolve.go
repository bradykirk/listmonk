package brands

import (
	"fmt"
	"sort"
)

type ListBrand struct {
	Slug      string
	Ambiguous bool
}

func BrandOfList(tags []string, brands []Brand) ListBrand {
	var found string
	for _, b := range brands {
		if hasAny(tags, b.ListTags) {
			if found != "" && found != b.Slug {
				return ListBrand{Ambiguous: true}
			}
			found = b.Slug
		}
	}
	return ListBrand{Slug: found}
}

func hasAny(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

func BrandListIDs(slug string, listTags map[int][]string, brands []Brand) []int {
	out := []int{}
	for id, tags := range listTags {
		if lb := BrandOfList(tags, brands); lb.Slug == slug {
			out = append(out, id)
		}
	}
	sort.Ints(out)
	return out
}

func CampaignBrand(listIDs []int, listTags map[int][]string, brands []Brand) (string, string) {
	if len(listIDs) == 0 {
		return "", "The campaign has no lists."
	}
	var slug string
	var first int
	for _, id := range listIDs {
		tags, ok := listTags[id]
		if !ok || id <= 0 {
			return "", fmt.Sprintf("List %d no longer exists.", id)
		}
		lb := BrandOfList(tags, brands)
		switch {
		case lb.Ambiguous:
			return "", fmt.Sprintf("List %d has tags of more than one brand.", id)
		case lb.Slug == "":
			return "", fmt.Sprintf("List %d belongs to no brand.", id)
		case slug == "":
			slug, first = lb.Slug, id
		case lb.Slug != slug:
			return "", fmt.Sprintf("List %d belongs to %s, but list %d belongs to %s.", first, slug, id, lb.Slug)
		}
	}
	return slug, ""
}
