// 版本更新服务 - 调用后端API
import { callBackend } from '../api/runtime';
import { onEvent } from '../api/events';

export interface UpdateInfo {
  hasUpdate: boolean;
  latestVersion: string;
  currentVersion: string;
  releaseUrl: string;
  releaseNotes: string;
  error?: string;
}

export interface UpdateProgress {
  status: 'checking' | 'downloading' | 'installing' | 'completed' | 'error';
  message: string;
  percent: number;
}

export async function checkForUpdate(): Promise<UpdateInfo> {
  return await callBackend<UpdateInfo>('CheckForUpdate', '/api/update/check');
}

export async function doUpdate(): Promise<string> {
  return await callBackend<string>('DoUpdate', '/api/update/apply');
}

export async function restartApp(): Promise<string> {
  return await callBackend<string>('RestartApp', '/api/update/restart');
}

export async function getCurrentVersion(): Promise<string> {
  return await callBackend<string>('GetCurrentVersion', '/api/version');
}

export function onUpdateProgress(callback: (progress: UpdateProgress) => void): () => void {
  return onEvent('update:progress', callback);
}
