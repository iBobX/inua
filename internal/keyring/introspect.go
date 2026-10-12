package keyring

import (
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	serviceXML = `<interface name="org.freedesktop.Secret.Service">
  <property name="Collections" type="ao" access="read"/>
  <method name="OpenSession"><arg name="algorithm" type="s" direction="in"/><arg name="input" type="v" direction="in"/><arg name="output" type="v" direction="out"/><arg name="result" type="o" direction="out"/></method>
  <method name="CreateCollection"><arg name="properties" type="a{sv}" direction="in"/><arg name="alias" type="s" direction="in"/><arg name="collection" type="o" direction="out"/><arg name="prompt" type="o" direction="out"/></method>
  <method name="SearchItems"><arg name="attributes" type="a{ss}" direction="in"/><arg name="unlocked" type="ao" direction="out"/><arg name="locked" type="ao" direction="out"/></method>
  <method name="Unlock"><arg name="objects" type="ao" direction="in"/><arg name="unlocked" type="ao" direction="out"/><arg name="prompt" type="o" direction="out"/></method>
  <method name="Lock"><arg name="objects" type="ao" direction="in"/><arg name="locked" type="ao" direction="out"/><arg name="Prompt" type="o" direction="out"/></method>
  <method name="GetSecrets"><arg name="items" type="ao" direction="in"/><arg name="session" type="o" direction="in"/><arg name="secrets" type="a{o(oayays)}" direction="out"/></method>
  <method name="ReadAlias"><arg name="name" type="s" direction="in"/><arg name="collection" type="o" direction="out"/></method>
  <method name="SetAlias"><arg name="name" type="s" direction="in"/><arg name="collection" type="o" direction="in"/></method>
  <signal name="CollectionCreated"><arg name="collection" type="o"/></signal>
  <signal name="CollectionDeleted"><arg name="collection" type="o"/></signal>
  <signal name="CollectionChanged"><arg name="collection" type="o"/></signal>
 </interface>`
	collectionXML = `<interface name="org.freedesktop.Secret.Collection">
  <property name="Items" type="ao" access="read"/>
  <property name="Label" type="s" access="readwrite"/>
  <property name="Locked" type="b" access="read"/>
  <property name="Created" type="t" access="read"/>
  <property name="Modified" type="t" access="read"/>
  <method name="Delete"><arg name="prompt" type="o" direction="out"/></method>
  <method name="SearchItems"><arg name="attributes" type="a{ss}" direction="in"/><arg name="results" type="ao" direction="out"/></method>
  <method name="CreateItem"><arg name="properties" type="a{sv}" direction="in"/><arg name="secret" type="(oayays)" direction="in"/><arg name="replace" type="b" direction="in"/><arg name="item" type="o" direction="out"/><arg name="prompt" type="o" direction="out"/></method>
  <signal name="ItemCreated"><arg name="item" type="o"/></signal>
  <signal name="ItemDeleted"><arg name="item" type="o"/></signal>
  <signal name="ItemChanged"><arg name="item" type="o"/></signal>
 </interface>`
	itemXML = `<interface name="org.freedesktop.Secret.Item">
  <property name="Locked" type="b" access="read"/>
  <property name="Attributes" type="a{ss}" access="readwrite"/>
  <property name="Label" type="s" access="readwrite"/>
  <property name="Created" type="t" access="read"/>
  <property name="Modified" type="t" access="read"/>
  <method name="Delete"><arg name="Prompt" type="o" direction="out"/></method>
  <method name="GetSecret"><arg name="session" type="o" direction="in"/><arg name="secret" type="(oayays)" direction="out"/></method>
  <method name="SetSecret"><arg name="secret" type="(oayays)" direction="in"/></method>
 </interface>`
	sessionXML = `<interface name="org.freedesktop.Secret.Session">
  <method name="Close"/>
 </interface>`
)

type introspectObj struct{ s *Service }

// Introspect describes the object at the path and lists its children.
func (o introspectObj) Introspect(m dbus.Message) (string, *dbus.Error) {
	p := msgPath(m)
	var b strings.Builder
	b.WriteString(introspect.IntrospectDeclarationString + "<node>\n " + introspect.IntrospectDataString + prop)
	node := func(name string) { b.WriteString(` <node name="` + name + `"/>` + "\n") }
	switch k, coll, _ := o.s.parse(p); {
	case k == kindService:
		b.WriteString(serviceXML)
		node("collection")
		node("aliases")
		node("session")
	case p == basePath+"/collection":
		for _, c := range o.s.store.Collections() {
			node(c.ID)
		}
	case p == basePath+"/aliases":
		for _, name := range []string{"default", Session} {
			if o.s.store.Alias(name) != "" {
				node(name)
			}
		}
	case p == basePath+"/session":
		o.s.mu.Lock()
		for sp := range o.s.sessions {
			node(strings.TrimPrefix(string(sp), string(basePath)+"/session/"))
		}
		o.s.mu.Unlock()
	case k == kindCollection:
		b.WriteString(collectionXML)
		items, _ := o.s.store.Items(coll)
		for _, it := range items {
			node(it.ID)
		}
	case k == kindItem:
		b.WriteString(itemXML)
	case k == kindSession:
		b.WriteString(sessionXML)
	default:
		return "", errNoObject
	}
	b.WriteString("</node>")
	return b.String(), nil
}

// prop describes org.freedesktop.DBus.Properties.
const prop = `<interface name="org.freedesktop.DBus.Properties">
  <method name="Get"><arg name="interface" type="s" direction="in"/><arg name="property" type="s" direction="in"/><arg name="value" type="v" direction="out"/></method>
  <method name="GetAll"><arg name="interface" type="s" direction="in"/><arg name="properties" type="a{sv}" direction="out"/></method>
  <method name="Set"><arg name="interface" type="s" direction="in"/><arg name="property" type="s" direction="in"/><arg name="value" type="v" direction="in"/></method>
  <signal name="PropertiesChanged"><arg name="interface" type="s"/><arg name="changed_properties" type="a{sv}"/><arg name="invalidated_properties" type="as"/></signal>
 </interface>
`
