package findings

// pick chooses what a review tells about from the findings of a player,
// which come the heaviest first: MaxMistakes mistakes at most, and the
// heaviest strength.
//
// A group gives one mistake, its heaviest: the rules of a group are much
// about the same events, and they would fill the review with one thing told
// three ways. Rules nested in one another tell it more and less exactly:
// a death alone is an untraded death. If several of them fired, the review
// tells the most exact one and names the numbers of the others in its text,
// see Pick.With.
func pick(fs []Finding) (mistakes []Pick, strength *Pick) {
	mistakes = []Pick{}
	for i, f := range fs {
		if f.Kind == Strength {
			strength = &Pick{Finding: i, Weight: f.Weight}
			break
		}
	}
	taken := make(map[Group]bool)
	for i, f := range fs {
		if f.Kind != Mistake || taken[f.Group] {
			continue
		}
		if len(mistakes) == MaxMistakes {
			break
		}
		taken[f.Group] = true
		// f is the heaviest of its group. Its family is the findings of the
		// group nested with it, itself among them, the heaviest first.
		var family []int
		for j, g := range fs {
			if g.Kind == Mistake && g.Group == f.Group && rootOf(g.Rule) == rootOf(f.Rule) {
				family = append(family, j)
			}
		}
		// The most exact of the family is a nested rule: the heaviest one,
		// if several fired.
		told := i
		for _, j := range family {
			if parentOf(fs[j].Rule) != "" {
				told = j
				break
			}
		}
		p := Pick{Finding: told, Weight: f.Weight}
		for _, j := range family {
			if j != told {
				p.With = append(p.With, j)
			}
		}
		mistakes = append(mistakes, p)
	}
	return mistakes, strength
}

// rootOf returns the rule that id is nested in, id itself if none.
func rootOf(id RuleID) RuleID {
	if p := parentOf(id); p != "" {
		return p
	}
	return id
}
