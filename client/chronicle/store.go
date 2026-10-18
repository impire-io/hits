package chronicle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	chron "github.com/impire-io/chronicle/client"
	ccontract "github.com/impire-io/chronicle/contract"

	"github.com/impire-io/hits/contract"
)

// The declarations of hits-hq 02-DESIGN/the-store.md: the four types with
// their schemas, operations and preconditions, the three indexes, and the
// announcement. Init applies them to a store; Manifest publishes the same
// declaration as the install manifest chronicle-hq decision 0054 shapes.

// The indexes a HITS declares.
const (
	IndexText    = "text"
	IndexMeaning = "meaning"
	IndexRel     = "rel"
)

// The announcement: an instance of chronicle's solution type.
const (
	SolutionType     = "solution"
	AnnouncementPath = "solution/hits"
	SolutionHome     = "https://github.com/impire-io/hits-hq"
)

// The preconditions (chronicle-hq decision 0059): JSON Schemas the
// instance's current state must satisfy for the operation to apply.
const (
	notTombstoned = `{"not":{"properties":{"tombstoned":{"const":true}},"required":["tombstoned"]}}`
	active        = `{"properties":{"status":{"enum":["open","diagnosing","located"]}},"required":["status"]}`
	notTerminal   = `{"properties":{"status":{"enum":["open","diagnosing","located","blocked"]}},"required":["status"]}`
	isBlocked     = `{"properties":{"status":{"const":"blocked"}},"required":["status"]}`
	isClaimed     = `{"required":["claimed-by"]}`
	notClaimed    = `{"not":{"required":["claimed-by"]}}`
	notRetired    = `{"not":{"properties":{"retired":{"const":true}},"required":["retired"]}}`
	notShipped    = `{"not":{"anyOf":[{"properties":{"shipped":{"const":true}},"required":["shipped"]},{"properties":{"retired":{"const":true}},"required":["retired"]}]}}`
	createOnly    = `{"maxProperties":0}`
	anyObject     = `{"type":"object"}`
)

func allOf(schemas ...string) json.RawMessage {
	out := `{"allOf":[`
	for i, s := range schemas {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return json.RawMessage(out + "]}")
}

func op(schema, effect string, precondition json.RawMessage) ccontract.OpDef {
	return ccontract.OpDef{Schema: json.RawMessage(schema), Effect: effect, Precondition: precondition}
}

// itemSchema states the shapes every item state keeps (the-store.md § item):
// `closed` exactly when the status is terminal, `located-in` present on a
// located item and on a task.
var itemSchema = json.RawMessage(fmt.Sprintf(`{
  "type": "object",
  "required": ["type", "status", "priority", "report", "reporter", "created"],
  "properties": {
    "type": {"enum": ["bug", "task", "improvement"]},
    "status": {"enum": ["open", "diagnosing", "located", "blocked", "resolved", "wontfix"]},
    "priority": {"enum": ["high", "normal", "low"]},
    "report": {"type": "string", "maxLength": %d},
    "discovered-while": {"type": "string", "maxLength": %d},
    "located-in": {"type": "array", "items": {"type": "string"}},
    "links": {"type": "object"},
    "tombstoned": {"type": "boolean"}
  },
  "allOf": [
    {"if": {"properties": {"status": {"enum": ["resolved", "wontfix"]}}}, "then": {"required": ["closed"]}, "else": {"not": {"required": ["closed"]}}},
    {"if": {"properties": {"status": {"const": "located"}}}, "then": {"properties": {"located-in": {"minItems": 1}}, "required": ["located-in"]}},
    {"if": {"properties": {"type": {"const": "task"}}}, "then": {"properties": {"located-in": {"minItems": 1}}, "required": ["located-in"]}}
  ]
}`, contract.MaxBodyBytes, contract.MaxLabelBytes))

// TypeDeclaration is one type as Init defines it, in order.
type TypeDeclaration struct {
	Name string
	Def  chron.TypeDefinition
}

// TypeDeclarations are the store's types in definition order: a release
// first, because an initiative names it as a child.
func TypeDeclarations() []TypeDeclaration {
	merge, none := ccontract.EffectMerge, ccontract.EffectNone
	full := ccontract.HistoryFull
	vocab := json.RawMessage(`{"type":"object","required":["slug","name"]}`)
	return []TypeDeclaration{
		{contract.TypeRelease, chron.TypeDefinition{
			Schema: vocab, History: full,
			Operations: map[string]ccontract.OpDef{
				"create": op(anyObject, merge, json.RawMessage(createOnly)),
				"ship":   op(`{"type":"object","required":["shipped","ship-refs"]}`, merge, json.RawMessage(notShipped)),
				"retire": op(`{"type":"object","required":["retired","retire-reason"]}`, merge, json.RawMessage(notShipped)),
			}}},
		{contract.TypeInitiative, chron.TypeDefinition{
			Schema: vocab, History: full,
			Children: map[string]string{contract.ReleasesChild: contract.TypeRelease},
			Operations: map[string]ccontract.OpDef{
				"create": op(anyObject, merge, json.RawMessage(createOnly)),
				"retire": op(`{"type":"object","required":["retired","retire-reason"]}`, merge, json.RawMessage(notRetired)),
				"mint":   op(`{"type":"object","required":["next"],"properties":{"next":{"type":"integer","minimum":1}}}`, merge, nil),
			}}},
		{contract.TypeProject, chron.TypeDefinition{
			// A legacy project was registered before initiatives existed and
			// assigned one later: the initiative is state, not a requirement.
			Schema: vocab, History: full,
			Operations: map[string]ccontract.OpDef{
				"create": op(anyObject, merge, json.RawMessage(createOnly)),
				"assign": op(`{"type":"object","required":["initiative"]}`, merge, json.RawMessage(notRetired)),
				"retire": op(`{"type":"object","required":["retired","retire-reason"]}`, merge, json.RawMessage(notRetired)),
			}}},
		{contract.TypeItem, chron.TypeDefinition{
			Schema: itemSchema, History: full,
			Operations: map[string]ccontract.OpDef{
				"create":     op(`{"type":"object","required":["type","status","report","reporter","created"]}`, merge, json.RawMessage(createOnly)),
				"note":       op(fmt.Sprintf(`{"type":"object","required":["text"],"properties":{"text":{"type":"string","maxLength":%d}}}`, contract.MaxBodyBytes), none, json.RawMessage(notTombstoned)),
				"edit":       op(anyObject, merge, allOf(notTombstoned, notTerminal)),
				"assign":     op(`{"type":"object","required":["initiative"]}`, merge, json.RawMessage(notTombstoned)),
				"transition": op(`{"type":"object","required":["status"]}`, merge, allOf(notTombstoned, active)),
				"claim":      op(`{"type":"object","required":["claimed-by","claimed"]}`, merge, allOf(notTombstoned, notTerminal, notClaimed)),
				"steal":      op(`{"type":"object","required":["stolen-from"]}`, merge, allOf(notTombstoned, notTerminal, isClaimed)),
				"release":    op(anyObject, merge, allOf(notTombstoned, isClaimed)),
				"block":      op(`{"type":"object","required":["status","interrupted"]}`, merge, allOf(notTombstoned, active)),
				"unblock":    op(`{"type":"object","required":["status"]}`, merge, allOf(notTombstoned, isBlocked)),
				"link":       op(`{"type":"object","required":["links"]}`, merge, json.RawMessage(notTombstoned)),
				"unlink":     op(`{"type":"object","required":["links"]}`, merge, json.RawMessage(notTombstoned)),
				"tombstone":  op(`{"type":"object","required":["tombstoned","tombstone-reason"]}`, merge, json.RawMessage(notTombstoned)),
			}}},
		{SolutionType, chron.TypeDefinition{
			Schema: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"},"description":{"type":"string"},"url":{"type":"string"},"version":{"type":"string"},"home":{"type":"string"}}}`),
			Operations: map[string]ccontract.OpDef{
				"create": op(`{"type":"object","required":["name"]}`, merge, json.RawMessage(createOnly)),
				"locate": op(`{"type":"object","required":["url"]}`, merge, nil),
			}}},
	}
}

// IndexDeclaration is one index as Init declares it.
type IndexDeclaration struct {
	Name   string
	Kind   string
	Config json.RawMessage
}

// IndexDeclarations are the three indexes of the-store.md § the indexes.
func IndexDeclarations() []IndexDeclaration {
	return []IndexDeclaration{
		{IndexText, "search", json.RawMessage(`{"source":"history","types":["create","note"]}`)},
		{IndexMeaning, "semantic", json.RawMessage(`{"source":"history","types":["create","note"],"fields":["report","text"]}`)},
		{IndexRel, "graph", json.RawMessage(`{"edges":[
			{"field":"links.duplicates","label":"duplicates"},
			{"field":"links.relates-to","label":"relates-to"},
			{"field":"located-in","label":"located-in"},
			{"field":"initiative","label":"in-initiative"},
			{"field":"target","label":"targets"},
			{"field":"reporter","label":"reported-by"},
			{"field":"claimed-by","label":"claimed-by"},
			{"field":"blocked-by","label":"blocked-by"}]}`)},
	}
}

// Announcement is the solution instance's state: what the console lists.
func Announcement(version string) map[string]any {
	return map[string]any{
		"name":        "hits",
		"description": "Headless, agent-native issue tracking: items with a lifecycle, filed and worked by agents and people, kept in this store",
		"version":     version,
		"home":        SolutionHome,
	}
}

// Init declares a HITS in the store: the store with full history, the
// types in order, the indexes, and the announcement — every step
// create-if-absent, so running it again finishes what was left and
// running it on a store that holds items changes only the declarations
// (hits-hq 02-DESIGN/the-cutover.md § hits store init).
func Init(ctx context.Context, cc *chron.Client, store, version string) error {
	if _, err := cc.CreateStore(ctx, store, "HITS: items, projects, initiatives, releases", chron.WithHistory(ccontract.HistoryFull)); err != nil && !isCode(err, ccontract.CodeStoreExists) {
		return fmt.Errorf("create store %s: %w", store, err)
	}
	for _, t := range TypeDeclarations() {
		if _, err := cc.DefineType(ctx, store, t.Name, t.Def); err != nil {
			return fmt.Errorf("define type %s: %w", t.Name, err)
		}
	}
	for _, ix := range IndexDeclarations() {
		if _, err := cc.DeclareIndex(ctx, store, ix.Name, ix.Kind, ix.Config); err != nil && !isCode(err, ccontract.CodeIndexExists) {
			return fmt.Errorf("declare index %s: %w", ix.Name, err)
		}
	}
	data, err := json.Marshal(Announcement(version))
	if err != nil {
		return err
	}
	// Announced already: the create-only guard, or the create's own
	// precondition at pre-flight, says so.
	if _, err := cc.Create(ctx, store, AnnouncementPath, "create", data, chron.Verified()); err != nil && !errors.Is(err, chron.ErrInstanceExists) && !errors.Is(err, chron.ErrPreconditionFailed) {
		return fmt.Errorf("announce: %w", err)
	}
	return nil
}

func isCode(err error, code string) bool {
	var serr *chron.ServiceError
	return errors.As(err, &serr) && serr.Code == code
}

// Manifest is the install manifest (chronicle-hq design 14 § the install
// manifest): the same declaration Init applies, as data, so a hosted
// console can enable a HITS for an account. The url is empty: HITS has no
// interface of its own.
func Manifest(version string) ([]byte, error) {
	type typeEntry struct {
		Name       string         `json:"name"`
		Definition map[string]any `json:"definition"`
	}
	types := make([]typeEntry, 0)
	for _, t := range TypeDeclarations() {
		def := map[string]any{"schema": t.Def.Schema, "operations": t.Def.Operations}
		if t.Def.History != "" {
			def["history"] = t.Def.History
		}
		if len(t.Def.Children) > 0 {
			def["children"] = t.Def.Children
		}
		types = append(types, typeEntry{t.Name, def})
	}
	ann := Announcement(version)
	m := map[string]any{
		"manifest": 1,
		"solution": map[string]any{"name": ann["name"], "description": ann["description"], "version": version, "home": SolutionHome, "url": ""},
		"stores": []map[string]any{{
			"name":        contract.StoreName,
			"description": "HITS: items, projects, initiatives, releases",
			"history":     ccontract.HistoryFull,
			"types":       types,
		}},
		"announce": map[string]any{"store": contract.StoreName, "path": AnnouncementPath},
	}
	return json.MarshalIndent(m, "", "  ")
}
