package structure

// Built-in definition keys, under the "vitals" reserved namespace —
// renamed from Lighthouse's own "lighthouse.*" prefix, otherwise
// unchanged. See 14-vitals-model.md for why ownership is the reserved-
// namespace convention rather than a separate OwnerKind field: any
// caller is free to register definitions outside this namespace for
// whatever vital types its own task or service needs — these are the
// common ones Archipelago ships so a caller doesn't have to redefine
// ordinary things, not a closed set.
const (
	BuiltinServiceLiveness     = "vitals.service.liveness"
	BuiltinServiceReadiness    = "vitals.service.readiness"
	BuiltinServiceUptime       = "vitals.service.uptime"
	BuiltinServiceMemoryUsage  = "vitals.service.memory_usage"
	BuiltinServiceCPUUsage     = "vitals.service.cpu_usage"
	BuiltinQueueDepth          = "vitals.queue.depth"
	BuiltinQueueOldestItemAge  = "vitals.queue.oldest_item_age"
	BuiltinQueueProcessingRate = "vitals.queue.processing_rate"
	BuiltinJobProgress         = "vitals.job.progress"
	BuiltinJobPhase            = "vitals.job.phase"
	BuiltinStorageUsed         = "vitals.storage.used"
	BuiltinStorageFree         = "vitals.storage.free"
	BuiltinStorageUsagePercent = "vitals.storage.usage_percent"
	BuiltinConnectionLiveness  = "vitals.connection.liveness"
	BuiltinConnectionLatency   = "vitals.connection.latency"
	BuiltinGenericHealth       = "vitals.generic.health"
	BuiltinGenericCount        = "vitals.generic.count"
	BuiltinGenericDuration     = "vitals.generic.duration"
	BuiltinGenericRate         = "vitals.generic.rate"
	BuiltinGenericPercent      = "vitals.generic.percent"
	BuiltinGenericBytes        = "vitals.generic.bytes"
)

func BuiltinDefinitionKeys() []string {
	return []string{
		BuiltinServiceLiveness,
		BuiltinServiceReadiness,
		BuiltinServiceUptime,
		BuiltinServiceMemoryUsage,
		BuiltinServiceCPUUsage,
		BuiltinQueueDepth,
		BuiltinQueueOldestItemAge,
		BuiltinQueueProcessingRate,
		BuiltinJobProgress,
		BuiltinJobPhase,
		BuiltinStorageUsed,
		BuiltinStorageFree,
		BuiltinStorageUsagePercent,
		BuiltinConnectionLiveness,
		BuiltinConnectionLatency,
		BuiltinGenericHealth,
		BuiltinGenericCount,
		BuiltinGenericDuration,
		BuiltinGenericRate,
		BuiltinGenericPercent,
		BuiltinGenericBytes,
	}
}
