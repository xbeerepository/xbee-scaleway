module github.com/iodasolutions/scaleway

go 1.25.0

require (
	github.com/iodasolutions/xbee-common v0.0.0-00010101000000-000000000000
	github.com/scaleway/scaleway-sdk-go v1.0.0-beta.36
)

require (
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/ulikunitz/xz v0.5.12 // indirect
	golang.org/x/crypto v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/iodasolutions/xbee-common => ../xbee-common
