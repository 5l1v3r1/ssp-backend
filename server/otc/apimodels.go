package otc

import (
	"github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"time"
)

type NewECSCommand struct {
	ECSName            string `json:"ecsName"`
	AvailabilityZone   string `json:"availabilityZone"`
	FlavorName         string `json:"flavorName"`
	ImageId            string `json:"imageId"`
	Billing            string `json:"billing"`
	PublicKey          string `json:"publicKey"`
	RootVolumeTypeId   string `json:"rootVolumeTypeId"`
	RootDiskSize       int    `json:"rootDiskSize"`
	SystemVolumeTypeId string `json:"systemVolumeTypeId"`
	SystemDiskSize     int    `json:"systemDiskSize"`
	DataVolumeTypeId   string `json:"dataVolumeTypeId"`
	DataDiskSize       int    `json:"dataDiskSize"`
	MegaId             string `json:"megaId"`
}

type DataDisk struct {
	DiskSize     int    `json:"diskSize"`
	VolumeTypeId string `json:"volumeTypeId"`
}

type FlavorListResponse struct {
	Flavors []Flavor `json:"flavors"`
}

type Flavor struct {
	Name  string `json:"name"`
	VCPUs int    `json:"vcpus"`
	RAM   int    `json:"ram"`
}

type AvailabilityZoneListResponse struct {
	AvailabilityZones []string `json:"availabilityZones"`
}

type ImageListResponse struct {
	Images []Image `json:"images"`
}

type Image struct {
	Name             string `json:"name"`
	Id               string `json:"id"`
	MinDiskGigabytes int    `json:"minDiskGigabytes"`
	MinRAMMegabytes  int    `json:"minRAMMegabytes"`
}

type ECServerListResponse struct {
	ECServers []ECServer `json:"ecServers"`
}

type ECServer struct {
	Id               string           `json:"id"`
	IPv4             []string         `json:"ipv4"`
	AvailabilityZone string           `json:"availabilityZone"`
	Name             string           `json:"name"`
	Created          time.Time        `json:"created"`
	VCPUs            int              `json:"vcpus"`
	RAM              int              `json:"ram"`
	ImageName        string           `json:"imageName"`
	Status           string           `json:"status"`
	Billing          string           `json:"billing"`
	Owner            string           `json:"owner"`
	MegaId           string           `json:"megaId"`
	Volumes          []volumes.Volume `json:"volumes"`
}

type VolumeTypesListResponse struct {
	VolumeTypes []VolumeType `json:"volumeTypes"`
}

type VolumeType struct {
	Name string `json:"name"`
	Id   string `json:"id"`
}

type ProjectName struct {
	Project string `json:"project"`
}

type NewS3BucketCommand struct {
	ProjectName
	BucketName string `json:"bucketname"`
	Billing    string `json:"billing"`
	Stage      string `json:"stage"`
}

type NewS3UserCommand struct {
	UserName   string `json:"username"`
	IsReadonly bool   `json:"isReadonly"`
}
