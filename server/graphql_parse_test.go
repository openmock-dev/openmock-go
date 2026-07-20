package server

import "testing"

func TestSelectOperation(t *testing.T) {
	cases := []struct {
		name          string
		query         string
		operationName string
		wantType      string
		wantName      string
		wantErr       bool
	}{
		{
			name:     "named query",
			query:    `query GetOrder($id: ID!) { order(id: $id) { id } }`,
			wantType: "query", wantName: "GetOrder",
		},
		{
			name:     "named mutation",
			query:    `mutation CancelOrder($id: ID!) { cancelOrder(id: $id) { ok } }`,
			wantType: "mutation", wantName: "CancelOrder",
		},
		{
			name:     "anonymous shorthand is a query",
			query:    `{ me { name } }`,
			wantType: "query", wantName: "",
		},
		{
			name:     "query keyword without a name",
			query:    `query { me { name } }`,
			wantType: "query", wantName: "",
		},
		{
			name:          "select by operationName among several",
			query:         `query A { a } mutation B { b }`,
			operationName: "B",
			wantType:      "mutation", wantName: "B",
		},
		{
			name:     "leading comment and block string arg do not confuse the scan",
			query:    "# pick the order\nquery GetOrder {\n  order(note: \"\"\"a } { b\"\"\") { id }\n}",
			wantType: "query", wantName: "GetOrder",
		},
		{
			name:     "string arg containing braces and hashes",
			query:    `mutation M { create(x: "a { # b } c") { id } }`,
			wantType: "mutation", wantName: "M",
		},
		{
			name:     "object default value in variable definitions",
			query:    `query Q($f: Filter = {state: OPEN, tags: ["a"]}) { items { id } }`,
			wantType: "query", wantName: "Q",
		},
		{
			name:     "directive with arguments before the body",
			query:    `query Q @cacheControl(maxAge: 60) { me { id } }`,
			wantType: "query", wantName: "Q",
		},
		{
			name:          "fragment is not an operation",
			query:         `fragment F on Order { id } query GetOrder { ...F }`,
			operationName: "GetOrder",
			wantType:      "query", wantName: "GetOrder",
		},
		{
			name:    "multiple operations without operationName is ambiguous",
			query:   `query A { a } query B { b }`,
			wantErr: true,
		},
		{
			name:          "operationName not found",
			query:         `query A { a }`,
			operationName: "Z",
			wantErr:       true,
		},
		{
			name:    "empty query has no operation",
			query:   `   `,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opType, opName, err := selectOperation(tc.query, tc.operationName)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("selectOperation = (%q,%q), want error", opType, opName)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectOperation: %v", err)
			}
			if opType != tc.wantType || opName != tc.wantName {
				t.Errorf("selectOperation = (%q,%q), want (%q,%q)", opType, opName, tc.wantType, tc.wantName)
			}
		})
	}
}
