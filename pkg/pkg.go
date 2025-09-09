package pkg

import (
	_ "plus/pkg/repo/deb"
	_ "plus/pkg/repo/rpm"
    _ "plus/pkg/repo/files"
	_ "plus/pkg/storage/local"
	_ "plus/pkg/storage/s3"
)
