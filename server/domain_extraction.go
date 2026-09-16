package server

import "manifest/domainextract"

func (s *Server) UseDomainExtraction(r *domainextract.Router) { s.domainExtraction = r }
