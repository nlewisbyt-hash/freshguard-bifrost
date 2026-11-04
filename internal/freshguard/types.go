package freshguard

import "time"

type Policy string

const (
	PolicyAllow Policy = "allow"
	PolicyWarn  Policy = "warn"
	PolicyBlock Policy = "block"
)

type Claim struct {
	Text   string `json:"text"`
	Domain string `json:"domain"`
}

type Evidence struct {
	URL         string    `json:"url"`
	Title       string    `json:"title"`
	Snippet     string    `json:"snippet"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	Supports    bool      `json:"supports"`
	Entailment  float64   `json:"entailment"`
}

type ScoreComponents struct {
	Recency         int `json:"recency"`
	SourceAgreement int `json:"source_agreement"`
	Entailment      int `json:"entailment"`
	Coverage        int `json:"coverage"`
}

type ClaimResult struct {
	Claim      Claim           `json:"claim"`
	Evidence   []Evidence      `json:"evidence"`
	Status     string          `json:"status"`
	Score      int             `json:"score"`
	Components ScoreComponents `json:"components"`
}

type VerificationReport struct {
	Status  string        `json:"status"`
	Results []ClaimResult `json:"results"`
}

type Config struct {
	Enabled    bool
	APIKey     string
	Policy     Policy
	Threshold  int
	MaxClaims  int
	MaxQueries int
	Timeout    time.Duration
}
