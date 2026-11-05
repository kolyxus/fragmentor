package demo

// The events of a match, for the metrics of internal/metrics. Like the
// summary, they cover the match itself: what happens before its last start,
// during a warmup and after its end is left out. Kills, hits, shots and
// flashes keep the order of the demo; sightings come in the order they
// began.

// Ref is a player or a bot as an event saw them.
type Ref struct {
	// ID is the SteamID64 of a player. A bot has none: its ID is its user
	// ID on the server plus one, which tells bots apart within a round.
	ID  uint64 `json:"id,string"`
	Bot bool   `json:"bot,omitempty"`
	// Side is the side at the moment, "" if neither T nor CT.
	Side Side `json:"side"`
	// Controller is the SteamID64 of the player who controlled the bot at
	// the moment, if one did: a dead player may take over a bot of their
	// team. Only the bodies of the events have it (Kill.Victim, Hit.Victim,
	// Shot.Shooter, the refs of Sighting and Flash); the killer, the
	// assister and the attacker of a bot's deeds are that player himself.
	Controller uint64 `json:"controller,string,omitempty"`
}

// Kill is a death of a player or a bot: by an enemy, a teammate, the world
// or their own hand. The game kills a player who leaves the server alive
// by their own hand, see Disconnect.
type Kill struct {
	Tick int `json:"tick"`
	// Round is the Number of the round the kill happened in: the round in
	// progress, or the one just ended until the next one starts.
	Round int `json:"round"`
	// AfterRoundEnd means the round was already won: players may still
	// kill each other until the next round starts, and the bomb kills
	// after the end of its round.
	AfterRoundEnd bool `json:"after_round_end,omitempty"`
	// Killer is nil for the world: the bomb, a fall. For a bot that a
	// player controls the killer is that player.
	Killer *Ref `json:"killer"`
	// Victim is whose body died: a bot that a player controlled is the
	// bot, with Ref.Controller set.
	Victim   Ref  `json:"victim"`
	Assister *Ref `json:"assister,omitempty"`
	// FlashAssist means the assister blinded the victim with a flash.
	FlashAssist bool `json:"flash_assist,omitempty"`
	Headshot    bool `json:"headshot,omitempty"`
	// Disconnect means the victim left the server, a player or a bot that
	// was kicked: the game counts it as a death by their own hand.
	Disconnect bool        `json:"disconnect,omitempty"`
	Weapon     string      `json:"weapon"` // like "AK-47" or "World"
	Class      WeaponClass `json:"class"`
	// Grenades is what the victim's body carried as it died, a name for
	// each grenade: two flashbangs are two, see Flashbang and the like.
	Grenades []string `json:"grenades,omitempty"`
}

// Shot is a shot of a gun: a pistol, an SMG, a shotgun, a rifle, a sniper
// rifle or a machine gun. Knives, the zeus and grenades make no shots.
type Shot struct {
	Tick          int  `json:"tick"`
	Round         int  `json:"round"` // as in Kill
	AfterRoundEnd bool `json:"after_round_end,omitempty"`
	// Shooter is whose body fired: a bot that a player controlled is the
	// bot, with Ref.Controller set.
	Shooter Ref         `json:"shooter"`
	Weapon  string      `json:"weapon"`
	Class   WeaponClass `json:"class"`
	// Speed is how fast the shooter moved over the ground, units per
	// second. MaxSpeed is the top speed with the gun at the moment, lower
	// scoped in; 0 for a gun of unknown speed.
	Speed    float64 `json:"speed"`
	MaxSpeed float64 `json:"max_speed"`
}

// Sighting is a stretch of time in which a player or a bot saw an enemy, as
// the game's spotted flag tells: it is set when the enemy is in sight, the
// flag of the radar. A sighting ends when the flag goes, when one of the
// two dies or when the next round starts, and it belongs to the round it
// began in. From and To are ticks, both included.
type Sighting struct {
	Round   int `json:"round"`
	Spotter Ref `json:"spotter"` // whose body saw, as in Shot.Shooter
	Target  Ref `json:"target"`  // whose body was seen
	From    int `json:"from"`
	To      int `json:"to"`
}

// Flash is a living player or bot blinded by a flashbang.
type Flash struct {
	Tick          int  `json:"tick"`
	Round         int  `json:"round"` // as in Kill
	AfterRoundEnd bool `json:"after_round_end,omitempty"`
	// Thrower is whose body threw the flashbang, as in Shot.Shooter; nil
	// if the demo does not tell.
	Thrower *Ref `json:"thrower"`
	// Victim is whose body was blinded, a bot with Ref.Controller set for
	// a bot that a player controlled.
	Victim Ref `json:"victim"`
	// Duration is how long the victim was blind, in seconds: the full
	// blindness, without the fading.
	Duration float64 `json:"duration"`
}

// Hit is a hit on a player or a bot: a bullet, a grenade, fire, a fall.
type Hit struct {
	Tick          int  `json:"tick"`
	Round         int  `json:"round"` // as in Kill
	AfterRoundEnd bool `json:"after_round_end,omitempty"`
	// Attacker is nil for the world. For a bot that a player controls the
	// attacker is that player.
	Attacker *Ref `json:"attacker"`
	Victim   Ref  `json:"victim"`
	// Damage is the health the hit took, no more than the victim had.
	Damage   int         `json:"damage"`
	HitGroup HitGroup    `json:"hit_group"`
	Weapon   string      `json:"weapon"`
	Class    WeaponClass `json:"class"`
}

// WeaponClass is the kind of a weapon.
type WeaponClass string

// The weapon classes.
const (
	ClassPistol     WeaponClass = "pistol"
	ClassSMG        WeaponClass = "smg"
	ClassShotgun    WeaponClass = "shotgun"
	ClassMachineGun WeaponClass = "machine_gun"
	ClassRifle      WeaponClass = "rifle"
	ClassSniper     WeaponClass = "sniper" // AWP, SSG 08, SCAR-20, G3SG1
	ClassKnife      WeaponClass = "knife"
	ClassZeus       WeaponClass = "zeus"
	ClassGrenade    WeaponClass = "grenade" // HE, and the hit of any other grenade
	ClassFire       WeaponClass = "fire"    // molotov, incendiary
	ClassBomb       WeaponClass = "bomb"
	ClassWorld      WeaponClass = "world" // a fall, leaving the server
	ClassOther      WeaponClass = "other"
)

// HitGroup is the part of the body a hit landed on.
type HitGroup string

// The hit groups. The neck takes the damage of the chest, not of the head:
// on FACEIT demos an AK-47 takes 30 from the neck and 123 from the head.
const (
	HitGeneric  HitGroup = "generic" // grenades, fire, falls
	HitHead     HitGroup = "head"
	HitChest    HitGroup = "chest"
	HitStomach  HitGroup = "stomach"
	HitLeftArm  HitGroup = "left_arm"
	HitRightArm HitGroup = "right_arm"
	HitLeftLeg  HitGroup = "left_leg"
	HitRightLeg HitGroup = "right_leg"
	HitNeck     HitGroup = "neck"
	HitGear     HitGroup = "gear"
)
