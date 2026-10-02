package chatgptplan

func (s *Service) cancelBrowser(owner, authorizationURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.flows[owner]; authorizationURL != "" && p != nil && p.authorizationURL == authorizationURL && p.flow.Status != "approved" {
		delete(s.flows, owner)
	}
}

func (s *Service) failBrowser(owner, authorizationURL, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.flows[owner]
	if authorizationURL == "" {
		if p != nil && (p.flow.Status == "authorization_required" || p.flow.Status == "starting") {
			return
		}
		p = &pending{owner: owner, expires: s.now().Add(flowTTL)}
		s.flows[owner] = p
	} else if p == nil || p.authorizationURL != authorizationURL || p.flow.Status != "authorization_required" {
		return
	}
	p.state, p.flow.AuthURL, p.flow.Status, p.err = "", "", "error", message
}
