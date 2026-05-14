package consts

type Effect int

const (
	EffectNotApplicable Effect = iota
	EffectAllow
	EffectDeny
)
