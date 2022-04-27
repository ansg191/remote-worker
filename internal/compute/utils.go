package compute

func remove[T any](slice []T, i int) []T {
	return append(slice[:i], slice[i+1:]...)
}

func removeItem[T Worker](slice []T, item T) []T {
	i := find(slice, item)
	if i < 0 {
		return slice
	} else {
		return remove(slice, i)
	}
}

func find[T Worker](slice []T, item T) int {
	for i, t := range slice {
		if item.Equals(t) {
			return i
		}
	}

	return -1
}
