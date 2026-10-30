package remotetunnel

// legacyWARPNames are the CNAME, the policy, the app and the posture of the retired WARP leg, in
// the order retireWARP deletes them, named as Cloudflare shows them.
func legacyWARPNames(s *State) []AbandonedResource {
	return []AbandonedResource{
		{Kind: "dns", Name: s.Resources.LegacyWARPHostname},
		{Kind: "access_policy", Name: resourceName(s, "warp-members")},
		{Kind: "access_app", Name: resourceName(s, "warp")},
		{Kind: "posture", Name: resourceName(s, "gateway")},
	}
}

// AbandonedOf lists what a row would leave in Cloudflare if its token could no longer reach the
// account: every recorded resource that teardown would delete, named as the operator will see it.
// The zone and the OTP provider are never deleted, so they are never listed. The result is never
// nil, so that a caller can tell an empty list from no abandonment at all.
func AbandonedOf(s State) []AbandonedResource {
	r := s.Resources
	out := []AbandonedResource{}
	for _, item := range []struct {
		id string
		AbandonedResource
	}{
		{r.TunnelID, AbandonedResource{Kind: "tunnel", Name: s.TunnelName}},
		{r.PublicDNSID, AbandonedResource{Kind: "dns", Name: s.OwnedHostname}},
		{r.PublicAppID, AbandonedResource{Kind: "access_app", Name: resourceName(&s, "public")}},
		{r.PublicPolicyID, AbandonedResource{Kind: "access_policy", Name: resourceName(&s, "public-members")}},
	} {
		if item.id != "" {
			out = append(out, item.AbandonedResource)
		}
	}
	legacy := legacyWARPNames(&s)
	for i, id := range []string{r.WARPDNSID, r.WARPPolicyID, r.WARPAppID, r.GatewayPostureID} {
		if id != "" {
			out = append(out, legacy[i])
		}
	}
	return out
}
