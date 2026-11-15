package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Label is the cached catalog of one account's user labels (Gmail only
// today). It is refreshed from the provider; Thread.labels keeps holding
// provider label IDs, which is what this table maps to names and colors.
type Label struct {
	ent.Schema
}

func (Label) Fields() []ent.Field {
	return []ent.Field{
		// Provider-native identity (Gmail "Label_203"). The name may contain
		// "/", which the UI renders as nesting.
		field.String("provider_label_id").NotEmpty(),
		field.String("name").NotEmpty(),
		// "#rrggbb" or empty when the label has no color.
		field.String("color_bg").Default(""),
		field.String("color_text").Default(""),
	}
}

func (Label) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("account", Account.Type).Ref("labels").Unique().Required(),
	}
}

func (Label) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider_label_id").Edges("account").Unique(),
	}
}
