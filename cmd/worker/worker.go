package main

import (
	"context"
	"runtime"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/jaypipes/ghw"
	"github.com/jaypipes/ghw/pkg/pci"
	"github.com/jaypipes/pcidb"
	"go.uber.org/zap"

	"golang.anshulg.com/popcorntime/go_encoder/api/proto"
)

type WorkerServer struct {
	proto.UnimplementedWorkerServiceServer
	logger *zap.Logger
}

func (w *WorkerServer) Status(context.Context, *proto.WorkerStatusRequest) (*proto.WorkerStatusResponse, error) {
	return &proto.WorkerStatusResponse{Msg: "OK"}, nil
}

//goland:noinspection GoBoolExpressions
func (w *WorkerServer) Info(_ context.Context, req *proto.WorkerInfoRequest) (*proto.WorkerInfoResponse, error) {
	res := &proto.WorkerInfoResponse{}

	// Memory
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		memory, err := ghw.Memory()
		if err != nil {
			return nil, err
		}

		res.Memory = &proto.MemoryInfo{
			Physical: uint64(memory.TotalPhysicalBytes),
			Usable:   uint64(memory.TotalUsableBytes),
		}
	}

	// CPU
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		cpu, err := ghw.CPU()
		if err != nil {
			return nil, err
		}

		info := &proto.CPUInfo{
			Cores:      cpu.TotalCores,
			Threads:    cpu.TotalThreads,
			Processors: nil,
		}

		for _, proc := range cpu.Processors {
			procInfo := &proto.CPUInfo_Processor{
				Id:           int32(proc.ID),
				NumCores:     proc.NumCores,
				NumThreads:   proc.NumThreads,
				Vendor:       proc.Vendor,
				Model:        proc.Model,
				Capabilities: proc.Capabilities,
				Cores:        nil,
			}

			for _, core := range proc.Cores {
				coreInfo := &proto.CPUInfo_Processor_Core{
					Id:                int32(core.ID),
					Index:             int32(core.Index),
					NumThreads:        core.NumThreads,
					LogicalProcessors: nil,
				}

				for _, lProc := range core.LogicalProcessors {
					coreInfo.LogicalProcessors = append(coreInfo.LogicalProcessors, int32(lProc))
				}

				procInfo.Cores = append(procInfo.Cores, coreInfo)
			}

			info.Processors = append(info.Processors, procInfo)
		}

		res.Cpu = info
	}

	// Storage
	{
		block, err := ghw.Block()
		if err != nil {
			return nil, err
		}

		info := &proto.StorageInfo{
			TotalBytes: block.TotalPhysicalBytes,
			Disks:      nil,
		}

		for _, disk := range block.Disks {
			diskInfo := &proto.StorageInfo_Disk{
				Name:                   disk.Name,
				SizeBytes:              disk.SizeBytes,
				PhysicalBlockSizeBytes: disk.PhysicalBlockSizeBytes,
				IsRemovable:            disk.IsRemovable,
				DriveType:              0,
				StorageController:      0,
				NumaNodeID:             int32(disk.NUMANodeID),
				Vendor:                 disk.Vendor,
				Model:                  disk.Model,
				SerialNumber:           disk.SerialNumber,
				Wwn:                    disk.WWN,
			}

			switch disk.DriveType {
			case ghw.DRIVE_TYPE_UNKNOWN:
				diskInfo.DriveType = proto.DriveType_UNKNOWN
			case ghw.DRIVE_TYPE_HDD:
				diskInfo.DriveType = proto.DriveType_HDD
			case ghw.DRIVE_TYPE_FDD:
				diskInfo.DriveType = proto.DriveType_FDD
			case ghw.DRIVE_TYPE_ODD:
				diskInfo.DriveType = proto.DriveType_ODD
			case ghw.DRIVE_TYPE_SSD:
				diskInfo.DriveType = proto.DriveType_SSD
			}

			switch disk.StorageController {
			case ghw.STORAGE_CONTROLLER_UNKNOWN:
				diskInfo.StorageController = proto.StorageInfo_Disk_UNKNOWN
			case ghw.STORAGE_CONTROLLER_IDE:
				diskInfo.StorageController = proto.StorageInfo_Disk_IDE
			case ghw.STORAGE_CONTROLLER_SCSI:
				diskInfo.StorageController = proto.StorageInfo_Disk_SCSI
			case ghw.STORAGE_CONTROLLER_NVME:
				diskInfo.StorageController = proto.StorageInfo_Disk_NVME
			case ghw.STORAGE_CONTROLLER_VIRTIO:
				diskInfo.StorageController = proto.StorageInfo_Disk_VIRTIO
			case ghw.STORAGE_CONTROLLER_MMC:
				diskInfo.StorageController = proto.StorageInfo_Disk_MMC
			}

			for _, part := range disk.Partitions {
				partInfo := &proto.StorageInfo_Disk_Partition{
					Name:       part.Name,
					SizeBytes:  part.SizeBytes,
					MountPoint: part.MountPoint,
					Type:       part.Type,
					IsReadOnly: part.IsReadOnly,
					Uuid:       part.UUID,
				}

				diskInfo.Partitions = append(diskInfo.Partitions, partInfo)
			}

			info.Disks = append(info.Disks, diskInfo)
		}

		res.Storage = info
	}

	// Topology
	//if runtime.GOOS == "linux" {
	//	topology, err := ghw.Topology()
	//	if err != nil {
	//		return nil, err
	//	}
	//}

	// Network
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		net, err := ghw.Network()
		if err != nil {
			return nil, err
		}

		info := &proto.NetworkInfo{}

		for _, c := range net.NICs {
			cInfo := &proto.NetworkInfo_NIC{
				Name:         c.Name,
				MacAddress:   c.MacAddress,
				IsVirtual:    c.IsVirtual,
				Capabilities: nil,
				PCIAddress:   aws.ToString(c.PCIAddress),
			}

			for _, capability := range c.Capabilities {
				capInfo := &proto.NetworkInfo_NIC_NICCapability{
					Name:      capability.Name,
					IsEnabled: capability.IsEnabled,
					CanEnable: capability.CanEnable,
				}

				cInfo.Capabilities = append(cInfo.Capabilities, capInfo)
			}

			info.Controllers = append(info.Controllers, cInfo)
		}

		res.Network = info
	}

	// PCI
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		p, err := ghw.PCI()
		if err != nil {
			return nil, err
		}

		info := &proto.PCIInfo{}

		for _, device := range p.Devices {
			dInfo := pciDeviceToProto(req, device)

			info.Devices = append(info.Devices, dInfo)
		}

		res.Pci = info
	}

	//GPU
	if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
		gpu, err := ghw.GPU()
		if err != nil {
			return nil, err
		}

		info := &proto.GPUInfo{}

		if len(gpu.GraphicsCards) == 0 {
			// https://github.com/jaypipes/ghw/blob/2ea05cb6c17c12a04d287cdab9c23df095de52ae/pkg/gpu/gpu_linux.go#L31
			// GPU Device info might not be stored in /sys/class/drm. In this case, we need
			// to sift through the pci device information from above for class 03 Devices
			// (Display Controllers). See https://pci-ids.ucw.cz/read/PD/ for PCI class
			// information
			var i uint32 = 0
			for _, device := range res.Pci.Devices {
				if device.Class.Id == "03" {
					cardInfo := &proto.GPUInfo_Card{
						Index:   i,
						Address: device.Address,
						Device:  device,
					}
					i += 1
					info.Card = append(info.Card, cardInfo)
				}
			}
		} else {
			for _, card := range gpu.GraphicsCards {
				cardInfo := &proto.GPUInfo_Card{
					Index:   uint32(card.Index),
					Address: card.Address,
					Device:  pciDeviceToProto(nil, card.DeviceInfo),
				}

				info.Card = append(info.Card, cardInfo)
			}
		}

		res.Gpu = info
	}

	return res, nil
}

func pciDeviceToProto(req *proto.WorkerInfoRequest, device *pci.Device) *proto.PCIInfo_Device {
	info := &proto.PCIInfo_Device{
		Vendor:    pciVendorToProto(req, device.Vendor),
		Product:   pciProductToProto(device.Product),
		Subsystem: pciProductToProto(device.Subsystem),
		Class:     pciClassToProto(device.Class),
		Subclass:  pciSubClassToProto(device.Subclass),
		Pi:        pciPIToProto(device.ProgrammingInterface),
		Driver:    device.Driver,
		Address:   device.Address,
	}

	return info
}

func pciVendorToProto(req *proto.WorkerInfoRequest, vendor *pcidb.Vendor) *proto.PCIInfo_Vendor {
	info := &proto.PCIInfo_Vendor{
		Id:       vendor.ID,
		Name:     vendor.Name,
		Products: nil,
	}

	if req.IncludeVendorProducts {
		for _, product := range vendor.Products {
			info.Products = append(info.Products, pciProductToProto(product))
		}
	}

	return info
}

func pciProductToProto(product *pcidb.Product) *proto.PCIInfo_Vendor_Product {
	info := &proto.PCIInfo_Vendor_Product{
		VendorId:   product.VendorID,
		Id:         product.ID,
		Name:       product.Name,
		Subsystems: nil,
	}

	for _, sub := range product.Subsystems {
		info.Subsystems = append(info.Subsystems, pciProductToProto(sub))
	}

	return info
}

func pciClassToProto(class *pcidb.Class) *proto.PCIInfo_Class {
	info := &proto.PCIInfo_Class{
		Id:         class.ID,
		Name:       class.Name,
		Subclasses: nil,
	}

	for _, sub := range class.Subclasses {
		info.Subclasses = append(info.Subclasses, pciSubClassToProto(sub))
	}

	return info
}

func pciSubClassToProto(subclass *pcidb.Subclass) *proto.PCIInfo_Class_SubClass {
	info := &proto.PCIInfo_Class_SubClass{
		Id:          subclass.ID,
		Name:        subclass.Name,
		PInterfaces: nil,
	}

	for _, pi := range subclass.ProgrammingInterfaces {
		info.PInterfaces = append(info.PInterfaces, pciPIToProto(pi))
	}

	return info
}

func pciPIToProto(pi *pcidb.ProgrammingInterface) *proto.PCIInfo_Class_ProgrammingInterface {
	return &proto.PCIInfo_Class_ProgrammingInterface{
		Id:   pi.ID,
		Name: pi.Name,
	}
}
