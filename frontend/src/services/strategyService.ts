// 策略与 Agent 配置服务 - 调用后端API
import { callBackend } from '../api/runtime';

// 策略专属专家配置
export interface StrategyAgent {
  id: string;
  name: string;
  role: string;
  avatar: string;
  color: string;
  instruction: string;
  tools: string[];
  mcpServers: string[];
  enabled: boolean;
  aiConfigId: string;
}

export interface Strategy {
  id: string;
  name: string;
  description: string;
  color: string;
  agents: StrategyAgent[];
  isBuiltin: boolean;
  source: string;
  sourceMeta: string;
  createdAt: number;
}

export interface GenerateStrategyRequest {
  prompt: string;
}

export interface GenerateStrategyResponse {
  success: boolean;
  error?: string;
  strategy?: Strategy;
  reasoning?: string;
}

// 获取所有策略
export const getStrategies = async (): Promise<Strategy[]> => {
  return await callBackend<Strategy[]>('GetStrategies', '/api/strategies');
};

// 获取当前激活策略ID
export const getActiveStrategyID = async (): Promise<string> => {
  return await callBackend<string>('GetActiveStrategyID', '/api/strategies/active');
};

// 设置当前激活策略
export const setActiveStrategy = async (id: string): Promise<string> => {
  return await callBackend<string>('SetActiveStrategy', '/api/strategies/active', [id], {
    query: { id },
    httpMethod: 'POST',
  });
};

// 添加策略
export const addStrategy = async (strategy: Strategy): Promise<string> => {
  return await callBackend<string>('AddStrategy', '/api/strategies', [strategy], {
    body: strategy,
    httpMethod: 'POST',
  });
};

// 更新策略
export const updateStrategy = async (strategy: Strategy): Promise<string> => {
  return await callBackend<string>('UpdateStrategy', '/api/strategies', [strategy], {
    body: strategy,
    httpMethod: 'PUT',
  });
};

// 删除策略
export const deleteStrategy = async (id: string): Promise<string> => {
  return await callBackend<string>('DeleteStrategy', '/api/strategies', [id], {
    query: { id },
    httpMethod: 'DELETE',
  });
};

// AI生成策略
export const generateStrategy = async (prompt: string): Promise<GenerateStrategyResponse> => {
  const req: GenerateStrategyRequest = { prompt };
  return await callBackend<GenerateStrategyResponse>('GenerateStrategy', '/api/strategies/generate', [req], {
    body: req,
  });
};

// 提示词增强请求
export interface EnhancePromptRequest {
  originalPrompt: string;
  agentRole: string;
  agentName: string;
}

// 提示词增强响应
export interface EnhancePromptResponse {
  success: boolean;
  enhancedPrompt?: string;
  error?: string;
}

// 增强Agent提示词
export const enhancePrompt = async (req: EnhancePromptRequest): Promise<EnhancePromptResponse> => {
  return await callBackend<EnhancePromptResponse>('EnhancePrompt', '/api/prompt/enhance', [req], {
    body: req,
  });
};

// ========== Agent Config API ==========

export interface AgentConfig {
  id: string;
  name: string;
  role: string;
  avatar: string;
  color: string;
  instruction: string;
  tools: string[];
  mcpServers: string[];
  enabled: boolean;
  aiConfigId: string;
}

// 获取所有已启用的Agent配置
export const getAgentConfigs = async (): Promise<AgentConfig[]> => {
  return await callBackend<AgentConfig[]>('GetAgentConfigs', '/api/agents');
};

// 添加Agent配置
export const addAgentConfig = async (config: AgentConfig): Promise<string> => {
  return await callBackend<string>('AddAgentConfig', '/api/agents', [config], {
    body: config,
    httpMethod: 'POST',
  });
};

// 更新Agent配置
export const updateAgentConfig = async (config: AgentConfig): Promise<string> => {
  return await callBackend<string>('UpdateAgentConfig', '/api/agents', [config], {
    body: config,
    httpMethod: 'PUT',
  });
};

// 删除Agent配置
export const deleteAgentConfig = async (id: string): Promise<string> => {
  return await callBackend<string>('DeleteAgentConfig', '/api/agents', [id], {
    query: { id },
    httpMethod: 'DELETE',
  });
};
