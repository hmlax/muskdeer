import { AxiosHeaders, type AxiosRequestConfig } from 'axios';

import type { RequestClient } from '../request-client';

class FileUploader {
  private client: RequestClient;

  constructor(client: RequestClient) {
    this.client = client;
  }

  public async upload(
    url: string,
    data: { file: Blob | File } & Record<string, unknown>,
    config?: AxiosRequestConfig,
  ): Promise<unknown> {
    const formData = new FormData();

    Object.entries(data).forEach(([key, value]) => {
      if (value === undefined || value === null) return;
      formData.append(
        key,
        value instanceof Blob
          ? value
          : typeof value === 'object'
            ? JSON.stringify(value)
            : String(value),
      );
    });

    // 注意：不要手动设置 Content-Type 为 'multipart/form-data'。
    // 浏览器在发送 FormData 时会自动生成带 boundary 的
    // 'multipart/form-data; boundary=----WebKitFormBoundaryXXX'，
    // 手动写死会导致 boundary 丢失，后端无法解析请求体。
    // 这里显式删除调用方可能误传的 Content-Type，确保由浏览器自动设置。
    const headers = new AxiosHeaders(
      config?.headers as ConstructorParameters<typeof AxiosHeaders>[0],
    );
    headers.delete('Content-Type');
    const finalConfig: AxiosRequestConfig = { ...config, headers };

    // 使用 request 方法代替 post 方法，避免类型推断问题
    return this.client.request(url, {
      method: 'POST',
      data: formData,
      ...finalConfig,
    });
  }
}

export { FileUploader };
