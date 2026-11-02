package llm

// Test-only shims around MessageConverter and the shared circuit-breaker
// registry. Production code depends on MessageConverter and the registry
// directly; these wrappers exist so existing tests keep compiling.

func (c *Client) validateMessageSequence(messages []Message) error {
	return c.messageConverter().ValidateMessageSequence(messages)
}

func (c *Client) cleanupMessages(messages []Message) []Message {
	return c.messageConverter().CleanupMessages(messages)
}

func resetCircuitBreakerRegistryForTest() {
	sharedCircuitBreakers.mu.Lock()
	defer sharedCircuitBreakers.mu.Unlock()
	sharedCircuitBreakers.breakers = make(map[string]*CircuitBreaker)
}
