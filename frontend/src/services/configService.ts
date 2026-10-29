// 配置服务 - 调用后端API
// 桌面模式走 Wails 绑定，Web 模式走 HTTP，两种模式共用同一组函数。
import { callBackend } from '../api/runtime';
import type { models } from '../../wailsjs/go/models';

export type AppConfig = models.AppConfig;

// 内置工具信息
export interface ToolInfo {
  name: string;
  description: string;
}

export const getConfig = async (): Promise<AppConfig> => {
  return await callBackend<AppConfig>('GetConfig', '/api/config');
};

export const updateConfig = async (config: AppConfig): Promise<string> => {
  return await callBackend<string>('UpdateConfig', '/api/config', [config], {
    body: config,
    httpMethod: 'PUT',
  });
};

// 获取可用的内置工具列表
export const getAvailableTools = async (): Promise<ToolInfo[]> => {
  return await callBackend<ToolInfo[]>('GetAvailableTools', '/api/tools');
};

// 测试 AI 配置连通性
export const testAIConnection = async (config: models.AIConfig): Promise<string> => {
  return await callBackend<string>('TestAIConnection', '/api/ai/test', [config], {
    body: config,
    httpMethod: 'POST',
  });
};
