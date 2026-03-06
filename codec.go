package tessera

import (
	"io"

	"github.com/aalpar/crdt/dotcontext"
	"github.com/aalpar/crdt/ormap"
)

// blockRefStoreCodec encodes the DotMap[string, *DotMap[string, *DotSet]] store.
var blockRefStoreCodec = &dotcontext.DotMapCodec[string, *dotcontext.DotMap[string, *dotcontext.DotSet]]{
	KeyCodec: dotcontext.StringCodec{},
	ValueCodec: &dotcontext.DotMapCodec[string, *dotcontext.DotSet]{
		KeyCodec:   dotcontext.StringCodec{},
		ValueCodec: dotcontext.DotSetCodec{},
	},
}

var blockRefCausalCodec = dotcontext.CausalCodec[*dotcontext.DotMap[string, *dotcontext.DotMap[string, *dotcontext.DotSet]]]{
	StoreCodec: blockRefStoreCodec,
}

// EncodeBlockRefDelta encodes a BlockRef delta for wire transport.
func EncodeBlockRefDelta(w io.Writer, delta *BlockRef) error {
	return blockRefCausalCodec.Encode(w, delta.inner.State())
}

// DecodeBlockRefDelta decodes a BlockRef delta from the wire.
func DecodeBlockRefDelta(r io.Reader) (*BlockRef, error) {
	causal, err := blockRefCausalCodec.Decode(r)
	if err != nil {
		return nil, err
	}
	return &BlockRef{
		inner: ormap.FromCausal(causal, joinInner, emptyInner),
	}, nil
}
