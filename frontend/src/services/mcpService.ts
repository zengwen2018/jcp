// MCP 服务 - 调用后端API
import { callBackend } from '../api/runtime';
import { models } from '../../wailsjs/go/models';

export type MCPServerConfig = models.MCPServerConfig;

// MCP 服务器状态
export interface MCPServerStatus {
  id: string;
  connected: boolean;
  error: string;
}

// MCP 工具信息
export interface MCPToolInfo {
  name: string;
  description: string;
  serverId: string;
  serverName: string;
}

export async function getMCPServers(): Promise<MCPServerConfig[]> {
  return await callBackend<MCPServerConfig[]>('GetMCPServers', '/api/mcp/servers');
}

export async function addMCPServer(server: MCPServerConfig): Promise<string> {
  return await callBackend<string>('AddMCPServer', '/api/mcp/servers', [server], { body: server });
}

export async function updateMCPServer(server: MCPServerConfig): Promise<string> {
  return await callBackend<string>('UpdateMCPServer', '/api/mcp/servers', [server], {
    body: server,
    httpMethod: 'PUT',
  });
}

export async function deleteMCPServer(id: string): Promise<string> {
  return await callBackend<string>('DeleteMCPServer', '/api/mcp/servers', [id], {
    query: { id },
    httpMethod: 'DELETE',
  });
}

// 获取所有 MCP 服务器状态
export async function getMCPStatus(): Promise<MCPServerStatus[]> {
  return await callBackend<MCPServerStatus[]>('GetMCPStatus', '/api/mcp/status');
}

// 测试指定 MCP 服务器连接
export async function testMCPConnection(serverID: string): Promise<MCPServerStatus> {
  return await callBackend<MCPServerStatus>('TestMCPConnection', '/api/mcp/test', [serverID], {
    query: { serverID },
  });
}

// 获取指定 MCP 服务器的工具列表
export async function getMCPServerTools(serverID: string): Promise<MCPToolInfo[]> {
  return await callBackend<MCPToolInfo[]>('GetMCPServerTools', '/api/mcp/tools', [serverID], {
    query: { serverID },
  });
}
