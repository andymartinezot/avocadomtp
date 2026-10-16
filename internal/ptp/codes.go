// Package ptp implements the byte-level encoding of PTP/MTP containers and datasets.
package ptp

import "fmt"

// Container types.
const (
	TypeCommand  uint16 = 1
	TypeData     uint16 = 2
	TypeResponse uint16 = 3
	TypeEvent    uint16 = 4
)

// Operation codes (PTP, MTP and Android extensions).
const (
	OpGetDeviceInfo      uint16 = 0x1001
	OpOpenSession        uint16 = 0x1002
	OpCloseSession       uint16 = 0x1003
	OpGetStorageIDs      uint16 = 0x1004
	OpGetStorageInfo     uint16 = 0x1005
	OpGetNumObjects      uint16 = 0x1006
	OpGetObjectHandles   uint16 = 0x1007
	OpGetObjectInfo      uint16 = 0x1008
	OpGetObject          uint16 = 0x1009
	OpDeleteObject       uint16 = 0x100B
	OpSendObjectInfo     uint16 = 0x100C
	OpSendObject         uint16 = 0x100D
	OpMoveObject         uint16 = 0x1019
	OpCopyObject         uint16 = 0x101A
	OpGetPartialObject   uint16 = 0x101B
	OpGetObjectPropsSupp uint16 = 0x9801
	OpGetObjectPropDesc  uint16 = 0x9802
	OpGetObjectPropValue uint16 = 0x9803
	OpSetObjectPropValue uint16 = 0x9804
	OpGetObjectPropList  uint16 = 0x9805
	OpGetPartialObject64 uint16 = 0x95C1 // Android
	OpSendPartialObject  uint16 = 0x95C2 // Android
	OpTruncateObject     uint16 = 0x95C3 // Android
	OpBeginEditObject    uint16 = 0x95C4 // Android
	OpEndEditObject      uint16 = 0x95C5 // Android
)

// Response codes.
const (
	RespOK                      uint16 = 0x2001
	RespGeneralError            uint16 = 0x2002
	RespSessionNotOpen          uint16 = 0x2003
	RespInvalidTransactionID    uint16 = 0x2004
	RespOperationNotSupported   uint16 = 0x2005
	RespParameterNotSupported   uint16 = 0x2006
	RespIncompleteTransfer      uint16 = 0x2007
	RespInvalidStorageID        uint16 = 0x2008
	RespInvalidObjectHandle     uint16 = 0x2009
	RespDevicePropNotSupported  uint16 = 0x200A
	RespInvalidObjectFormatCode uint16 = 0x200B
	RespStoreFull               uint16 = 0x200C
	RespObjectWriteProtected    uint16 = 0x200D
	RespStoreReadOnly           uint16 = 0x200E
	RespAccessDenied            uint16 = 0x200F
	RespNoThumbnailPresent      uint16 = 0x2010
	RespStoreNotAvailable       uint16 = 0x2013
	RespDeviceBusy              uint16 = 0x2019
	RespInvalidParentObject     uint16 = 0x201A
	RespInvalidParameter        uint16 = 0x201D
	RespSessionAlreadyOpen      uint16 = 0x201E
	RespTransactionCancelled    uint16 = 0x201F
	RespInvalidObjectPropCode   uint16 = 0xA801
	RespSpecByDepthUnsupported  uint16 = 0xA808
)

var respNames = map[uint16]string{
	RespOK:                      "OK",
	RespGeneralError:            "GeneralError",
	RespSessionNotOpen:          "SessionNotOpen",
	RespInvalidTransactionID:    "InvalidTransactionID",
	RespOperationNotSupported:   "OperationNotSupported",
	RespParameterNotSupported:   "ParameterNotSupported",
	RespIncompleteTransfer:      "IncompleteTransfer",
	RespInvalidStorageID:        "InvalidStorageID",
	RespInvalidObjectHandle:     "InvalidObjectHandle",
	RespDevicePropNotSupported:  "DevicePropNotSupported",
	RespInvalidObjectFormatCode: "InvalidObjectFormatCode",
	RespStoreFull:               "StoreFull",
	RespObjectWriteProtected:    "ObjectWriteProtected",
	RespStoreReadOnly:           "StoreReadOnly",
	RespAccessDenied:            "AccessDenied",
	RespNoThumbnailPresent:      "NoThumbnailPresent",
	RespStoreNotAvailable:       "StoreNotAvailable",
	RespDeviceBusy:              "DeviceBusy",
	RespInvalidParentObject:     "InvalidParentObject",
	RespInvalidParameter:        "InvalidParameter",
	RespSessionAlreadyOpen:      "SessionAlreadyOpen",
	RespTransactionCancelled:    "TransactionCancelled",
	RespInvalidObjectPropCode:   "InvalidObjectPropCode",
	RespSpecByDepthUnsupported:  "SpecificationByDepthUnsupported",
}

// RespName returns a human readable name for a response code.
func RespName(code uint16) string {
	if n, ok := respNames[code]; ok {
		return n
	}
	return fmt.Sprintf("0x%04X", code)
}

// Object format codes.
const (
	FormatUndefined   uint16 = 0x3000
	FormatAssociation uint16 = 0x3001 // folder
)

// Association types.
const AssocGenericFolder uint16 = 0x0001

// Object property codes.
const (
	PropStorageID      uint16 = 0xDC01
	PropObjectFormat   uint16 = 0xDC02
	PropObjectSize     uint16 = 0xDC04
	PropObjectFileName uint16 = 0xDC07
	PropDateCreated    uint16 = 0xDC08
	PropDateModified   uint16 = 0xDC09
	PropParentObject   uint16 = 0xDC0B
	PropName           uint16 = 0xDC44
)

// Data type codes used in property lists.
const (
	DTInt8    uint16 = 0x0001
	DTUint8   uint16 = 0x0002
	DTInt16   uint16 = 0x0003
	DTUint16  uint16 = 0x0004
	DTInt32   uint16 = 0x0005
	DTUint32  uint16 = 0x0006
	DTInt64   uint16 = 0x0007
	DTUint64  uint16 = 0x0008
	DTInt128  uint16 = 0x0009
	DTUint128 uint16 = 0x000A
	DTArray   uint16 = 0x4000 // OR-ed with the element type
	DTString  uint16 = 0xFFFF
)

// Special handle values.
const (
	HandleRoot    uint32 = 0xFFFFFFFF // "root of storage" as parent
	SizeUnknown32 uint32 = 0xFFFFFFFF // ObjectCompressedSize for objects >= 4 GiB
)
