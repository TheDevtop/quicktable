# Quicktable Datamodel

### Models

**Keyspace/Valueset:**
Represents a set of either keys or values.

```json
["mammalia", "carnivora", "felidae", "felis"]
```

**Value:**
Represents a single value.

```json
"felis"
```

**Key:**
Represents a reduced keyspace.

```json
"mammalia:carnivora:felidae:felis"
```

### Forms

**Keys form:**
A key exclusive form.

```json
{
    "keys": ["mammalia", "carnivora", "felidae", "felis"]
}
```

**Regular form:**
A regular key/value form.

```json
{
  "keys": ["mammalia", "carnivora", "felidae"],
  "values": ["felis"]
}
```

**Report form:**
A reporting form, where `T` can be any other model or form.

```json
{
  "failed": false,
  "mesg": "",
  "data": T
}
```
