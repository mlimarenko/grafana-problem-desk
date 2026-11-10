package policy

import (
	"sort"
	"strings"
)

// Compiled holds indexed lookups for one immutable policy revision.
type Compiled struct {
	Doc      *Document
	Revision int64

	levelsByID    map[string]*Level
	aliases       map[string]string
	workflowsByID map[string]*Workflow
	fallback      *Level
}

// Compile validates and indexes a document for the given revision.
func Compile(doc *Document, revision int64) (*Compiled, error) {
	if doc == nil {
		return nil, &ValidationError{Issues: []Issue{{Path: "$", Code: "required", Message: "policy document is required"}}}
	}
	if issues := Validate(doc); len(issues) > 0 {
		return nil, &ValidationError{Issues: issues}
	}

	compiled := &Compiled{
		Doc:           doc,
		Revision:      revision,
		levelsByID:    make(map[string]*Level, len(doc.Levels)),
		aliases:       make(map[string]string, len(doc.Levels)*4),
		workflowsByID: make(map[string]*Workflow, len(doc.Workflows)),
	}
	for i := range doc.Levels {
		level := &doc.Levels[i]
		compiled.levelsByID[level.ID] = level
		for _, alias := range level.Aliases {
			compiled.aliases[strings.ToLower(alias)] = level.ID
		}
	}
	for i := range doc.Workflows {
		workflow := &doc.Workflows[i]
		compiled.workflowsByID[workflow.ID] = workflow
	}

	compiled.fallback = compiled.selectFallback()
	if compiled.fallback == nil {
		return nil, &ValidationError{Issues: []Issue{{Path: "$.levels", Code: "required", Message: "at least one level is required"}}}
	}
	return compiled, nil
}

// selectFallback returns the level used when no alias matches: the level id
// "unknown" when configured, otherwise the lowest rank with id ascending.
func (c *Compiled) selectFallback() *Level {
	if unknown, ok := c.levelsByID["unknown"]; ok {
		return unknown
	}
	candidates := make([]*Level, 0, len(c.Doc.Levels))
	for i := range c.Doc.Levels {
		candidates = append(candidates, &c.Doc.Levels[i])
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Rank != candidates[j].Rank {
			return candidates[i].Rank < candidates[j].Rank
		}
		return candidates[i].ID < candidates[j].ID
	})
	if len(candidates) == 0 {
		return nil
	}
	return candidates[0]
}

// LevelMatch reports the resolved level and whether the raw value matched an alias.
type LevelMatch struct {
	Level   Level
	Matched bool
}

// ResolveLevel reads a raw source label value literally (no trimming) and
// resolves it through the configured aliases case-insensitively.
func (c *Compiled) ResolveLevel(raw string) LevelMatch {
	if levelID, ok := c.aliases[strings.ToLower(raw)]; ok {
		if level, exists := c.levelsByID[levelID]; exists {
			return LevelMatch{Level: *level, Matched: true}
		}
	}
	return LevelMatch{Level: *c.fallback, Matched: false}
}

// HasLevel reports whether the id belongs to this revision.
func (c *Compiled) HasLevel(id string) bool {
	_, ok := c.levelsByID[id]
	return ok
}

// Level returns a configured level by id.
func (c *Compiled) Level(id string) (Level, bool) {
	level, ok := c.levelsByID[id]
	if !ok {
		return Level{}, false
	}
	return *level, true
}

// SelectWorkflow applies the ordered routing rules, then defaultWorkflow.
func (c *Compiled) SelectWorkflow(labels map[string]string, levelID string) (string, bool) {
	for _, route := range c.Doc.Routing {
		if route.Match.Level != "" && route.Match.Level != levelID {
			continue
		}
		matches := true
		for key, want := range route.Match.Labels {
			got, ok := labels[key]
			if !ok || got != want {
				matches = false
				break
			}
		}
		if matches {
			return route.Workflow, true
		}
	}
	if c.Doc.DefaultWorkflow != nil && *c.Doc.DefaultWorkflow != "" {
		return *c.Doc.DefaultWorkflow, true
	}
	return "", false
}

// Workflow returns a workflow by id.
func (c *Compiled) Workflow(id string) (*Workflow, bool) {
	workflow, ok := c.workflowsByID[id]
	return workflow, ok
}

// InitialState returns the initial state of a workflow.
func (c *Compiled) InitialState(workflowID string) (*State, bool) {
	workflow, ok := c.workflowsByID[workflowID]
	if !ok {
		return nil, false
	}
	for i := range workflow.States {
		if workflow.States[i].ID == workflow.InitialState {
			return &workflow.States[i], true
		}
	}
	return nil, false
}

// State returns a state of a workflow.
func (c *Compiled) State(workflowID, stateID string) (*State, bool) {
	workflow, ok := c.workflowsByID[workflowID]
	if !ok {
		return nil, false
	}
	for i := range workflow.States {
		if workflow.States[i].ID == stateID {
			return &workflow.States[i], true
		}
	}
	return nil, false
}

// Transition returns the explicit edge from one state to another.
func (c *Compiled) Transition(workflowID, from, to string) (*Transition, bool) {
	workflow, ok := c.workflowsByID[workflowID]
	if !ok {
		return nil, false
	}
	for i := range workflow.Transitions {
		if workflow.Transitions[i].From == from && workflow.Transitions[i].To == to {
			return &workflow.Transitions[i], true
		}
	}
	return nil, false
}

// TransitionsFrom returns the ordered outgoing edges of a state.
func (c *Compiled) TransitionsFrom(workflowID, from string) []Transition {
	workflow, ok := c.workflowsByID[workflowID]
	if !ok {
		return nil
	}
	edges := make([]Transition, 0, len(workflow.Transitions))
	for _, transition := range workflow.Transitions {
		if transition.From == from {
			edges = append(edges, transition)
		}
	}
	return edges
}

// GateFacts are the facts evaluated against a Requires set.
type GateFacts struct {
	Acknowledged    bool
	Owner           string
	Comment         string
	SourceRecovered bool
}

// MissingGates returns unmet gate names in canonical order.
func MissingGates(requires Requires, facts GateFacts) []string {
	missing := make([]string, 0, 4)
	if requires.Ack && !facts.Acknowledged {
		missing = append(missing, "ack")
	}
	if requires.Owner && strings.TrimSpace(facts.Owner) == "" {
		missing = append(missing, "owner")
	}
	if requires.Comment && strings.TrimSpace(facts.Comment) == "" {
		missing = append(missing, "comment")
	}
	if requires.SourceRecovered && !facts.SourceRecovered {
		missing = append(missing, "sourceRecovered")
	}
	return missing
}
