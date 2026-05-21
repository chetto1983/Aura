package api

func materializedPages(wikiPages []string) []string {
	if len(wikiPages) <= 1 {
		return nil
	}
	out := make([]string, len(wikiPages)-1)
	copy(out, wikiPages[1:])
	return out
}
