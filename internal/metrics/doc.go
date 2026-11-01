// Package metrics aggregates API calls: buckets of usage and cost, the one-hour-cache what-if,
// cache-break causes and where a subagent run's time goes. Pure: it works on decoded values
// and never touches the filesystem. Port of the 1.x core/metrics, identical to the last bit.
package metrics
