package server

// Adapter capability truth for the supervision path. Every value here
// describes what an adapter's code path can honour, not a running delivery's
// state and not authority to invoke an external tool. The UI and the input
// handlers consult the same matrix: an action an adapter cannot honour is
// refused in words, never silently ignored or emulated.
//
// Vocabulary (string fields):
//
//	Queue           durable | none
//	Interrupt       request-and-cancel-queued | unsupported
//	Stop            request | process-kill | unsupported
//	Steer           unsupported | explicit          (explicit = steer:true on a working agent)
//	Retry           explicit-resubmit                (never automatic; an uncertain send is never replayed)
//	Resume          fresh-session-per-turn | exact-resume-id | tmux-relaunch | unsupported
//	AnswerQuestions unsupported | async-codex | terminal-only
//	Supervision     delivery-receipt | input-receipt+observation | observation-only
//	SkillInventory  on-disk | not-reported          (on-disk = skill folders readable now via …/skills; never what a turn loaded)
type chatCapabilities struct {
	Adapter             string `json:"adapter"`
	Queue               string `json:"queue"`
	CancelQueued        bool   `json:"cancelQueued"`
	Interrupt           string `json:"interrupt"`
	Stop                string `json:"stop"`
	Steer               string `json:"steer"`
	LiveSteering        bool   `json:"liveSteering"`
	Retry               string `json:"retry"`
	Resume              string `json:"resume"`
	StructuredQuestions bool   `json:"structuredQuestions"`
	AnswerQuestions     string `json:"answerQuestions"`
	Supervision         string `json:"supervision"`
	SkillInventory      string `json:"skillInventory"`
}

const (
	adapterHermesOneshot = "hermes-oneshot"
	adapterHerdrCodex    = "herdr-codex"
	adapterHerdrClaude   = "herdr-claude"
	adapterHerdrOther    = "herdr-shell"
	adapterTmuxLegacy    = "tmux-legacy"
	adapterRemoteKeep    = "remote-keep"
)

// nativeChatCapabilities: Manifest's durable delivery store in front of one
// `hermes -z` turn per instruction. Every turn is a fresh Hermes session, so
// there is nothing to resume; steering mid-turn is impossible (the CLI has no
// input channel); questions are not structured. Stop is a cancellation
// request whose already-started external effects may continue.
func nativeChatCapabilities() chatCapabilities {
	return chatCapabilities{
		Adapter: adapterHermesOneshot, Queue: "durable", CancelQueued: true,
		Interrupt: "request-and-cancel-queued", Stop: "request", Steer: "unsupported",
		Retry: "explicit-resubmit", Resume: "fresh-session-per-turn",
		AnswerQuestions: "unsupported", Supervision: "delivery-receipt", SkillInventory: "on-disk",
	}
}

// terminalChatCapabilities describes a coding runtime row. herdr sessions
// carry input receipts and a live observation; the chat outbox is their queue
// and steer:true their deliberate mid-turn send. Only Codex exposes async
// structured questions Manifest can answer; Claude prompts need Terminal.
// Legacy tmux rows have no receipts and no agent observation: keys and text
// only, and supervision is observation-only (never idle-means-done).
func terminalChatCapabilities(se termSession) chatCapabilities {
	if se.Device != "" {
		// Input is metis-local only; a kept remote session can be watched, not driven.
		return chatCapabilities{Adapter: adapterRemoteKeep, Queue: "none", Interrupt: "unsupported", Stop: "unsupported", Steer: "unsupported",
			Retry: "explicit-resubmit", Resume: "unsupported", AnswerQuestions: "unsupported", Supervision: "observation-only", SkillInventory: "not-reported"}
	}
	if se.backend() != "herdr" {
		return chatCapabilities{Adapter: adapterTmuxLegacy, Queue: "none", Interrupt: "unsupported", Stop: "process-kill", Steer: "unsupported",
			Retry: "explicit-resubmit", Resume: "tmux-relaunch", AnswerQuestions: "terminal-only", Supervision: "observation-only", SkillInventory: "not-reported"}
	}
	caps := chatCapabilities{Adapter: adapterHerdrOther, Queue: "none", Interrupt: "unsupported", Stop: "process-kill", Steer: "unsupported",
		Retry: "explicit-resubmit", Resume: "exact-resume-id", AnswerQuestions: "unsupported", Supervision: "input-receipt+observation", SkillInventory: "not-reported"}
	switch se.Kind {
	case "codex":
		caps.Adapter, caps.Queue, caps.CancelQueued, caps.Steer, caps.LiveSteering = adapterHerdrCodex, "durable", true, "explicit", true
		caps.StructuredQuestions, caps.AnswerQuestions = true, "async-codex"
		caps.SkillInventory = "on-disk"
	case "claude":
		caps.Adapter, caps.Queue, caps.CancelQueued, caps.Steer, caps.LiveSteering = adapterHerdrClaude, "durable", true, "explicit", true
		caps.AnswerQuestions = "terminal-only"
		caps.SkillInventory = "on-disk"
	}
	return caps
}

// chatAdapterCapabilities is the whole matrix, for tests and for the API that
// answers "what can this adapter do" without a live row.
func chatAdapterCapabilities() []chatCapabilities {
	return []chatCapabilities{
		nativeChatCapabilities(),
		terminalChatCapabilities(termSession{Backend: "herdr", Kind: "codex"}),
		terminalChatCapabilities(termSession{Backend: "herdr", Kind: "claude"}),
		terminalChatCapabilities(termSession{Kind: "claude"}),
		terminalChatCapabilities(termSession{Kind: "claude", Device: "laptop"}),
	}
}
