package rdapclient

// Common RDAP data structures and object classes per RFC 9083.

// Link represents an RDAP link object.
type Link struct {
	Value    string `json:"value,omitempty"`
	Rel      string `json:"rel,omitempty"`
	Href     string `json:"href,omitempty"`
	Type     string `json:"type,omitempty"`
	Title    string `json:"title,omitempty"`
	HrefLang string `json:"hreflang,omitempty"`
	Media    string `json:"media,omitempty"`
}

// Event represents an RDAP event object.
type Event struct {
	EventAction string `json:"eventAction"`
	EventDate   string `json:"eventDate"`
	EventActor  string `json:"eventActor,omitempty"`
	Links       []Link `json:"links,omitempty"`
}

// EventNoActor is used where the eventActor member is not present (asEventActor).
type EventNoActor struct {
	EventAction string `json:"eventAction"`
	EventDate   string `json:"eventDate"`
	Links       []Link `json:"links,omitempty"`
}

// Remark represents an RDAP remark object.
type Remark struct {
	Title       string   `json:"title,omitempty"`
	Type        string   `json:"type,omitempty"`
	Description []string `json:"description,omitempty"`
	Links       []Link   `json:"links,omitempty"`
}

// Notice represents an RDAP notice object (top-level informational messages).
type Notice struct {
	Title       string   `json:"title,omitempty"`
	Type        string   `json:"type,omitempty"`
	Description []string `json:"description,omitempty"`
	Links       []Link   `json:"links,omitempty"`
}

// PublicID represents a public identifier associated with an entity or domain.
type PublicID struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
}

// IPAddresses groups v4 and v6 addresses for nameservers.
type IPAddresses struct {
	V4 []string `json:"v4,omitempty"`
	V6 []string `json:"v6,omitempty"`
}

// CommonObject captures members common to all RDAP object classes and top-level responses.
// It is embedded in concrete object types to inline these fields in JSON.
type CommonObject struct {
	ObjectClassName string   `json:"objectClassName"`
	Handle          string   `json:"handle,omitempty"`
	Status          []string `json:"status,omitempty"`
	Entities        []Entity `json:"entities,omitempty"`
	Links           []Link   `json:"links,omitempty"`
	Remarks         []Remark `json:"remarks,omitempty"`
	Events          []Event  `json:"events,omitempty"`
	Port43          string   `json:"port43,omitempty"`

	// Top-level-only (but harmless if present elsewhere)
	RDAPConformance []string `json:"rdapConformance,omitempty"`
	Notices         []Notice `json:"notices,omitempty"`
}

// VariantName represents a single variant domain label.
type VariantName struct {
	LDHName     string `json:"ldhName,omitempty"`
	UnicodeName string `json:"unicodeName,omitempty"`
}

// Variant represents a set of IDN variants.
type Variant struct {
	Relation     []string      `json:"relation,omitempty"`
	IDNTable     string        `json:"idnTable,omitempty"`
	VariantNames []VariantName `json:"variantNames,omitempty"`
}

// DSData represents a Delegation Signer.
type DSData struct {
	KeyTag     int     `json:"keyTag"`
	Algorithm  int     `json:"algorithm"`
	Digest     string  `json:"digest"`
	DigestType int     `json:"digestType"`
	Links      []Link  `json:"links,omitempty"`
	Events     []Event `json:"events,omitempty"`
}

// KeyData represents a DNSKEY.
type KeyData struct {
	// Note: RFCs commonly use "flags"; some deployments use "flag". We model the RFC spelling.
	Flags     int     `json:"flags"`
	Protocol  int     `json:"protocol"`
	PublicKey string  `json:"publicKey"`
	Algorithm int     `json:"algorithm"`
	Links     []Link  `json:"links,omitempty"`
	Events    []Event `json:"events,omitempty"`
}

// SecureDNS represents DNSSEC information for a domain.
type SecureDNS struct {
	ZoneSigned       bool      `json:"zoneSigned,omitempty"`
	DelegationSigned bool      `json:"delegationSigned,omitempty"`
	DSData           []DSData  `json:"dsData,omitempty"`
	KeyData          []KeyData `json:"keyData,omitempty"`
}

// Entity represents the RDAP entity object class.
type Entity struct {
	CommonObject
	VCardArray   any            `json:"vcardArray,omitempty"`
	Roles        []string       `json:"roles,omitempty"`
	PublicIDs    []PublicID     `json:"publicIds,omitempty"`
	AsEventActor []EventNoActor `json:"asEventActor,omitempty"`
	Networks     []IPNetwork    `json:"networks,omitempty"`
	Autnums      []Autnum       `json:"autnums,omitempty"`
}

// Nameserver represents the RDAP nameserver object class.
type Nameserver struct {
	CommonObject
	LDHName     string       `json:"ldhName,omitempty"`
	UnicodeName string       `json:"unicodeName,omitempty"`
	IPAddresses *IPAddresses `json:"ipAddresses,omitempty"`
}

// Domain represents the RDAP domain object class.
type Domain struct {
	CommonObject
	LDHName     string       `json:"ldhName,omitempty"`
	UnicodeName string       `json:"unicodeName,omitempty"`
	Variants    []Variant    `json:"variants,omitempty"`
	Nameservers []Nameserver `json:"nameservers,omitempty"`
	SecureDNS   *SecureDNS   `json:"secureDNS,omitempty"`
	PublicIDs   []PublicID   `json:"publicIds,omitempty"`
	Network     *IPNetwork   `json:"network,omitempty"`
}

// IPNetwork represents the RDAP ip network object class.
type IPNetwork struct {
	CommonObject
	StartAddress string `json:"startAddress,omitempty"`
	EndAddress   string `json:"endAddress,omitempty"`
	IPVersion    string `json:"ipVersion,omitempty"`
	Name         string `json:"name,omitempty"`
	Type         string `json:"type,omitempty"`
	Country      string `json:"country,omitempty"`
	ParentHandle string `json:"parentHandle,omitempty"`
}

// Autnum represents the RDAP autnum object class.
type Autnum struct {
	CommonObject
	StartAutnum int64  `json:"startAutnum,omitempty"`
	EndAutnum   int64  `json:"endAutnum,omitempty"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Country     string `json:"country,omitempty"`
}

// GetObjectClassName returns the object class name for each concrete type.
func (o CommonObject) GetObjectClassName() string { return o.ObjectClassName }

// Validate ensures the embedded objectClassName matches the expected value.
func (e *Entity) Validate() bool     { return lower(e.ObjectClassName) == "entity" }
func (d *Domain) Validate() bool     { return lower(d.ObjectClassName) == "domain" }
func (n *Nameserver) Validate() bool { return lower(n.ObjectClassName) == "nameserver" }
func (i *IPNetwork) Validate() bool  { return lower(i.ObjectClassName) == "ip network" }
func (a *Autnum) Validate() bool     { return lower(a.ObjectClassName) == "autnum" }
