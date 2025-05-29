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
  "Keys": ["hotels", "trivago"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": "hotels:trivago"
}
```

### /api/index/ranged
Returns a range based on keys.

**Request:**
```json
{
  "Keys": ["hotels"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": [
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
  "Keys": ["hotels", "walast"],
  "Values": ["New York", "Amsterdam", "Dubai"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": "hotels:walast"
}
```

### /api/insert/ranged
Insert records on range.

**Request:**
```json
{
  "Keys": ["hotels"],
  "Values": ["Las Vegas"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": [
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
  "Keys": ["hotels", "hilton"],
  "Values": ["Singapore", "Tokyo"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": "hotels:hilton"
}
```

### /api/delete
Delete exactly one record.

**Request:**
```json
{
  "Keys": ["hotels", "trivago"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": "hotels:trivago"
}
```

### /api/delete/ranged
Delete a range of records

**Request:**
```json
{
  "Keys": ["hotels"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": null
}
```

### /api/query
Query an exact record.

**Request:**
```json
{
  "Keys": ["cities", "nl", "gd"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": [
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
  "Keys": ["cities", "nl"]
}
```

**Response:**
```json
{
  "Failed": false,
  "Mesg": "",
  "Data": {
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
