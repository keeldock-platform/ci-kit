package github

// Windows overlaps file write/append with directory add-file/add-subdirectory.
// Creation alone cannot replace an existing protected child; DELETE_CHILD can.
func pathMutationRights(directory bool) uint32 {
	const genericWrite = 0x40000000
	const genericAll = 0x10000000
	const deleteObject = 0x10000
	const writeDACL = 0x40000
	const writeOwner = 0x80000
	const writeData = 0x2
	const appendData = 0x4
	const deleteChild = 0x40
	const writeEA = 0x10
	const writeAttributes = 0x100
	mask := uint32(genericWrite | genericAll | deleteObject | writeDACL | writeOwner | writeEA | writeAttributes)
	if directory {
		return mask | deleteChild
	}
	return mask | writeData | appendData
}
