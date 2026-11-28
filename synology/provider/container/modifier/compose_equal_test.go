package modifier

import "testing"

func TestComposeSemanticallyEqual_IgnoresKeyOrder(t *testing.T) {
	t.Parallel()

	// Key order from a DSM-stored postgres project, then the same values in
	// the order a fresh render emits. No value changes.
	stored := `
services:
  db:
    container_name: db
    deploy:
      replicas: 1
    ports:
      - target: 5432
        published: "5432"
        protocol: tcp
    command:
      - postgres
    healthcheck:
      test:
        - CMD
        - pg_isready
      interval: 10s
`
	rendered := `
services:
  db:
    healthcheck:
      interval: 10s
      test:
        - CMD
        - pg_isready
    command:
      - postgres
    ports:
      - protocol: tcp
        published: "5432"
        target: 5432
    deploy:
      replicas: 1
    container_name: db
`
	equal, err := composeSemanticallyEqual(stored, rendered)
	if err != nil {
		t.Fatalf("composeSemanticallyEqual() error: %v", err)
	}
	if !equal {
		t.Fatal("reordered keys with the same values compared unequal")
	}
}

func TestComposeSemanticallyEqual_ValueAndSequenceChanges(t *testing.T) {
	t.Parallel()

	base := `
services:
  db:
    image: postgres:16
    command:
      - postgres
      - -c
      - max_connections=100
    ports:
      - published: "5432"
`
	cases := []struct {
		name  string
		other string
		equal bool
	}{
		{
			name: "changed image",
			other: `
services:
  db:
    image: postgres:17
    command:
      - postgres
      - -c
      - max_connections=100
    ports:
      - published: "5432"
`,
			equal: false,
		},
		{
			name: "reordered command arguments",
			other: `
services:
  db:
    image: postgres:16
    command:
      - -c
      - postgres
      - max_connections=100
    ports:
      - published: "5432"
`,
			equal: false,
		},
		{
			name: "same values reordered service keys",
			other: `
services:
  db:
    ports:
      - published: "5432"
    command:
      - postgres
      - -c
      - max_connections=100
    image: postgres:16
`,
			equal: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			equal, err := composeSemanticallyEqual(base, tt.other)
			if err != nil {
				t.Fatalf("composeSemanticallyEqual() error: %v", err)
			}
			if equal != tt.equal {
				t.Fatalf("composeSemanticallyEqual() = %v, want %v", equal, tt.equal)
			}
		})
	}
}

func TestComposeSemanticallyEqual_FileMode(t *testing.T) {
	t.Parallel()

	// DSM stores the decimal permission bits. Octal 0400 is 256. Decimal 400
	// is octal 0620 and must stay a difference.
	stored := `
services:
  app:
    secrets:
      - source: forgejo-db-password
        target: /run/secrets/db
        mode: 256
`
	cases := []struct {
		name  string
		other string
		equal bool
	}{
		{
			name: "decimal bits of octal 0400",
			other: `
services:
  app:
    secrets:
      - mode: 256
        target: /run/secrets/db
        source: forgejo-db-password
`,
			equal: true,
		},
		{
			name: "yaml octal 0400 is the same permission as 256",
			other: `
services:
  app:
    secrets:
      - source: forgejo-db-password
        target: /run/secrets/db
        mode: 0400
`,
			equal: true,
		},
		{
			name: "decimal 400 is a different permission",
			other: `
services:
  app:
    secrets:
      - source: forgejo-db-password
        target: /run/secrets/db
        mode: 400
`,
			equal: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			equal, err := composeSemanticallyEqual(stored, tt.other)
			if err != nil {
				t.Fatalf("composeSemanticallyEqual() error: %v", err)
			}
			if equal != tt.equal {
				t.Fatalf("composeSemanticallyEqual() = %v, want %v", equal, tt.equal)
			}
		})
	}
}

func TestComposeSemanticallyEqual_InvalidYAML(t *testing.T) {
	t.Parallel()

	if _, err := composeSemanticallyEqual("services: [", "services: {}\n"); err == nil {
		t.Fatal("composeSemanticallyEqual() succeeded, want a decode error")
	}
}
