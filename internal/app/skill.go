package app

// preToolSkill refuses the main thread's call to the Skill tool with nxy:feature or nxy:engineer:
// those start only when the user types the command, so Claude has to ask for it.
func (h Hooks) preToolSkill(ev HookEvent) HookResponse {
	if ev.AgentID != "" {
		return HookResponse{}
	}
	switch ev.Skill {
	case "nxy:feature", "nxy:engineer":
		cmd := "/" + ev.Skill
		return HookResponse{Deny: "Solo arranca tipeado por el usuario, no por un Skill. Decile en una línea que escriba " +
			cmd + " <pedido> al principio de su mensaje (en /nxy:feature <pedido> va lo que quiere hacer)."}
	}
	return HookResponse{}
}
