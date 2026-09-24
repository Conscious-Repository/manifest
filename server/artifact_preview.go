package server

import (
	"net/http"
	"unicode/utf8"
)

// Preview is opt-in so exact-content consumers retain their existing contract.
// Classification uses the selected immutable bytes, never a filename or head.
type artifactPreview struct {
	Revision  string `json:"revision"`
	Kind      string `json:"kind"`
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	Reason    string `json:"reason,omitempty"`
}

func describeArtifactPreview(hash string, b []byte) *artifactPreview {
	p := &artifactPreview{Revision: hash, Kind: "metadata", MediaType: http.DetectContentType(b), Size: len(b)}
	switch p.MediaType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		p.Kind = "image"
		return p
	case "application/pdf":
		p.Kind = "pdf"
		return p
	}
	if len(b) > 1024*1024 {
		p.Reason = "This file exceeds the 1 MiB text preview limit. Open the original file to inspect it."
		return p
	}
	if !utf8.Valid(b) {
		p.Reason = "This file is not UTF-8 text and has no supported inline preview. Open the original file to inspect it."
		return p
	}
	for _, c := range b {
		if c < 32 && c != '\n' && c != '\r' && c != '\t' {
			p.Reason = "This file contains binary control bytes and has no supported inline preview. Open the original file to inspect it."
			return p
		}
	}
	p.Kind = "text"
	return p
}
