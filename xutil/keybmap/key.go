package keybmap

import "github.com/BurntSushi/xgb/xproto"

type Key struct {
	km      *KeybMap
	Keycode xproto.Keycode // byte
	Mods    Modifiers
}

func newKey(km *KeybMap, keycode xproto.Keycode, state uint16) *Key {
	return &Key{km, keycode, Modifiers(state)}
}
func (k *Key) FirstKeysym() xproto.Keysym {
	return k.km.KeysymColumn(k.Keycode, 0)
}
func (k *Key) Keysym() xproto.Keysym {
	col := k.km.modifiersColumn(k.Mods)
	return k.km.KeysymColumn(k.Keycode, col)
}
