# Quicktable API

### /ping
Returns pong.

**Response:**
```json
"pong!"
```

### /api/index
Returns the exact key if it exists.

**Request:**
```json
{
  "keys": ["hotels", "trivago"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "hotels:trivago"
}
```

### /api/index/ranged
Returns a range based on keys.

**Request:**
```json
{
  "keys": ["hotels"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": [
    "hotels:hilton",
    "hotels:trivago",
    "hotels:vdvalk",
    "hotels:walast"
  ]
}
```

### /api/insert
Insert record with key.

**Request:**
```json
{
  "keys": ["hotels", "walast"],
  "values": ["New York", "Amsterdam", "Dubai"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "hotels:walast"
}
```

### /api/insert/ranged
Insert records on range.

**Request:**
```json
{
  "keys": ["hotels"],
  "values": ["Las Vegas"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": [
    "hotels:hilton",
    "hotels:trivago",
    "hotels:vdvalk",
    "hotels:walast"
  ]
}
```

### /api/append
Append values to record.

**Request:**
```json
{
  "keys": ["hotels", "hilton"],
  "values": ["Singapore", "Tokyo"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "hotels:hilton"
}
```

### /api/delete
Delete exactly one record.

**Request:**
```json
{
  "keys": ["hotels", "trivago"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "hotels:trivago"
}
```

### /api/delete/ranged
Delete a range of records

**Request:**
```json
{
  "keys": ["hotels"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": null
}
```

### /api/query
Query an exact record.

**Request:**
```json
{
  "keys": ["cities", "nl", "gd"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": [
    "Zutphen",
    "Nijmegen",
    "Arnhem",
    "Culemborg"
  ]
}
```

### /api/query/ranged
Query a range of records.

**Request:**
```json
{
  "keys": ["cities", "nl"]
}
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": {
    "cities:nl:gd": [
      "Zutphen",
      "Nijmegen",
      "Arnhem",
      "Culemborg"
    ],
    "cities:nl:nb": [
      "s-Hertogenbosch",
      "Breda",
      "Tilburg",
      "Eindhoven"
    ]
  }
}
```

### /api/generate/id
Generate a unique numeric id key.

**Request:**
```json
["orders"]
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "orders:000018"
}
```

### /api/generate/hash
Generate a random hash key.

**Request:**
```json
["metrics", "indices"]
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "metrics:indices:cc9d1e617122d8d8"
}
```

### /api/generate/key
Merge list into key.

**Request:**
```json
["cities", "nl"]
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": "cities:nl"
}
```

### /api/generate/list
Expand key into list.

**Request:**
```json
"cities:nl:gd"
```

**Response:**
```json
{
  "failed": false,
  "mesg": "",
  "data": [
    "cities",
    "nl",
    "gd"
  ]
}
```
