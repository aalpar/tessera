package tessera

import (
	"io"

	"github.com/aalpar/crdt/awset"
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

// appendEntryCodec encodes an appendEntry as [string: Hash] [int64: Timestamp] [string: ReplicaID] [uint64: Seq].
type appendEntryCodec struct{}

func (appendEntryCodec) Encode(w io.Writer, e appendEntry) error {
	if err := (dotcontext.StringCodec{}).Encode(w, e.Hash); err != nil {
		return err
	}
	if err := (dotcontext.Int64Codec{}).Encode(w, e.Timestamp); err != nil {
		return err
	}
	if err := (dotcontext.StringCodec{}).Encode(w, e.ReplicaID); err != nil {
		return err
	}
	return (dotcontext.Uint64Codec{}).Encode(w, e.Seq)
}

func (appendEntryCodec) Decode(r io.Reader) (appendEntry, error) {
	hash, err := (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return appendEntry{}, err
	}
	ts, err := (dotcontext.Int64Codec{}).Decode(r)
	if err != nil {
		return appendEntry{}, err
	}
	rid, err := (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return appendEntry{}, err
	}
	seq, err := (dotcontext.Uint64Codec{}).Decode(r)
	if err != nil {
		return appendEntry{}, err
	}
	return appendEntry{Hash: hash, Timestamp: ts, ReplicaID: rid, Seq: seq}, nil
}

var appendRecipeStoreCodec = &dotcontext.DotMapCodec[appendEntry, *dotcontext.DotSet]{
	KeyCodec:   appendEntryCodec{},
	ValueCodec: dotcontext.DotSetCodec{},
}

var appendRecipeCausalCodec = dotcontext.CausalCodec[*dotcontext.DotMap[appendEntry, *dotcontext.DotSet]]{
	StoreCodec: appendRecipeStoreCodec,
}

// patchEntryCodec encodes a patchEntry.
type patchEntryCodec struct{}

func (patchEntryCodec) Encode(w io.Writer, e patchEntry) error {
	if err := (dotcontext.StringCodec{}).Encode(w, e.FileID); err != nil {
		return err
	}
	if err := (dotcontext.Uint64Codec{}).Encode(w, e.Offset); err != nil {
		return err
	}
	if err := (dotcontext.Uint64Codec{}).Encode(w, e.Size); err != nil {
		return err
	}
	if err := (dotcontext.StringCodec{}).Encode(w, e.DataHash); err != nil {
		return err
	}
	if err := (dotcontext.Int64Codec{}).Encode(w, e.Timestamp); err != nil {
		return err
	}
	if err := (dotcontext.StringCodec{}).Encode(w, e.ReplicaID); err != nil {
		return err
	}
	return (dotcontext.Uint64Codec{}).Encode(w, e.Seq)
}

func (patchEntryCodec) Decode(r io.Reader) (patchEntry, error) {
	var e patchEntry
	var err error
	e.FileID, err = (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Offset, err = (dotcontext.Uint64Codec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Size, err = (dotcontext.Uint64Codec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.DataHash, err = (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Timestamp, err = (dotcontext.Int64Codec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.ReplicaID, err = (dotcontext.StringCodec{}).Decode(r)
	if err != nil {
		return e, err
	}
	e.Seq, err = (dotcontext.Uint64Codec{}).Decode(r)
	return e, err
}

var patchIndexStoreCodec = &dotcontext.DotMapCodec[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]{
	KeyCodec: dotcontext.StringCodec{},
	ValueCodec: &dotcontext.DotMapCodec[patchEntry, *dotcontext.DotSet]{
		KeyCodec:   patchEntryCodec{},
		ValueCodec: dotcontext.DotSetCodec{},
	},
}

var patchIndexCausalCodec = dotcontext.CausalCodec[*dotcontext.DotMap[string, *dotcontext.DotMap[patchEntry, *dotcontext.DotSet]]]{
	StoreCodec: patchIndexStoreCodec,
}

// EncodePatchIndexDelta encodes a PatchIndex delta for wire transport.
func EncodePatchIndexDelta(w io.Writer, delta *PatchIndex) error {
	return patchIndexCausalCodec.Encode(w, delta.inner.State())
}

// DecodePatchIndexDelta decodes a PatchIndex delta from the wire.
func DecodePatchIndexDelta(r io.Reader) (*PatchIndex, error) {
	causal, err := patchIndexCausalCodec.Decode(r)
	if err != nil {
		return nil, err
	}
	return &PatchIndex{
		inner: ormap.FromCausal(causal, joinPatchInner, emptyPatchInner),
	}, nil
}

// EncodeAppendRecipeDelta encodes an AppendRecipe delta for wire transport.
func EncodeAppendRecipeDelta(w io.Writer, delta *AppendRecipe) error {
	return appendRecipeCausalCodec.Encode(w, delta.set.State())
}

// DecodeAppendRecipeDelta decodes an AppendRecipe delta from the wire.
func DecodeAppendRecipeDelta(r io.Reader) (*AppendRecipe, error) {
	causal, err := appendRecipeCausalCodec.Decode(r)
	if err != nil {
		return nil, err
	}
	return &AppendRecipe{
		set: awset.FromCausal(causal),
	}, nil
}
