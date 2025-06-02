package client

import "github.com/TheDevtop/quicktable/pkg/shared/core"

type Conn struct {
	baseAddr string
}

func (c *Conn) List(str string) []string {
	return core.Expand(str)
}

func (c *Conn) Key(list ...string) string {
	return core.Merge(list...)
}

func (c *Conn) Index(keys []string) (string, error) {

}

func (c *Conn) IndexRanged(keys []string) ([]string, error) {

}

func (c *Conn) Insert(keys []string, values []string) (string, error) {

}

func (c *Conn) InsertRanged(keys []string, values []string) ([]string, error) {

}

func (c *Conn) Append(keys []string, values []string) (string, error) {

}

func (c *Conn) Copy(keys []string, values []string) (string, error) {

}

func (c *Conn) Move(keys []string, values []string) (string, error) {

}

func (c *Conn) Delete(keys []string) (string, error) {

}

func (c *Conn) DeleteRanged([]string) ([]string, error) {

}

func (c *Conn) Query(keys []string) ([]string, error) {

}

func (c *Conn) QueryRanged(keys []string) (map[string][]string, error) {

}

func (c *Conn) NewId(keys []string) (string, error) {

}

func (c *Conn) NewHash(keys []string) (string, error) {

}
