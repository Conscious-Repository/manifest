package writing

import "os"

func isMissing(err error) bool { return os.IsNotExist(err) }
