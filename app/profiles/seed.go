package profiles

// Seed writes the starter profiles (mapped to Benjamin's actual jobs, per the build
// plan §4) if the profiles dir is empty. Existing profiles are never overwritten.
func (s *Store) Seed() error {
	if len(s.List()) > 0 {
		return nil
	}
	for _, p := range seeded {
		if _, err := s.Save(p); err != nil {
			return err
		}
	}
	return nil
}

// The feed-item contract, embedded in scouts' briefs so their output materializes
// cleanly. The dashboard parses the LAST fenced ```json array of these objects.
const feedItemContract = "" +
	"When you have findings, end your reply with a single fenced ```json code block " +
	"containing a JSON array. Each element:\n" +
	"  {\"type\": \"paper|person|company|finding\", \"title\": \"...\", " +
	"\"why\": \"one line on why it matters to Benjamin\", \"link\": \"https://...\", " +
	"\"source\": \"where it came from\", \"domain\": \"e.g. bioelectricity\", " +
	"\"confidence\": \"low|medium|high\"}\n" +
	"Emit only real, verifiable items with working links. If nothing new, emit an empty array []."

var seeded = []Profile{
	{
		Name:        "domain-scout",
		Model:       "cheap",
		Tools:       []string{"web", "file"},
		Permissions: []string{"read-only"},
		Schedule:    "0 7 * * *",
		Brief: "Scan for new people, papers, companies, and findings relevant to Benjamin's " +
			"research domain (read his synced vault to learn the domain — bioelectricity, aging, " +
			"agentic AI, and adjacent areas). Produce a short feed of candidates, each one line: " +
			"what it is, why it matters, link, source. Do not write vault notes.\n\n" + feedItemContract,
	},
	{
		Name:        "options-scout",
		Model:       "strong",
		Tools:       []string{"web", "file"},
		Permissions: []string{"read-only"},
		Schedule:    "none",
		Brief: "Given a request like \"buy X amount of Y, find 5 options,\" research the market and " +
			"deliver a comparison artifact: options, price, key specs, pros/cons, links, source, and a " +
			"recommendation. Do not purchase or enter any credentials. Deliver the artifact for Benjamin " +
			"to review.\n\n" +
			"Return the artifact as a single fenced ```json block: a JSON array with ONE object of " +
			"{\"type\": \"artifact\", \"title\": \"...\", \"why\": \"the recommendation in one line\", " +
			"\"source\": \"...\", \"confidence\": \"low|medium|high\", \"body\": \"a markdown comparison " +
			"table of the 5 options with price/specs/pros-cons/links\"}.",
	},
	{
		Name:        "ea-coordinator",
		Model:       "strong",
		Tools:       []string{"calendar", "email", "web", "file"},
		Permissions: []string{"propose-only"},
		Schedule:    "none",
		Brief: "Executive-assistant tasks. Examples: \"draft a reply to X with time suggestions\" " +
			"(read the calendar for free slots, draft the reply); \"coordinate a refund on X\" (draft " +
			"the message + a step plan). Never send, pay, or move money yourself — every outgoing message " +
			"or irreversible step is a PROPOSAL for Benjamin to approve and send.\n\n" +
			"Put each proposed action in a single fenced ```json block: a JSON array of " +
			"{\"action\": \"short label, e.g. 'Send email to Lee'\", \"body\": \"the full draft / step " +
			"plan in markdown\"}. Draft only; do not act.",
	},
}
