import type { PluginOption } from 'vite';

import archiver from 'archiver';
import fs from 'fs';

/**
 * 构建完成后将 dist 目录打包为 dist.zip，便于交付部署
 *
 * apply: 'build' 确保 dev server 不加载本插件，仅生产构建生效
 */
export const archiverPlugin = (): PluginOption => {
  let outDir = '';
  return {
    apply: 'build',
    configResolved(config) {
      outDir = config.build.outDir;
    },
    closeBundle: {
      async handler() {
        await zipFolder(outDir, `${outDir}.zip`);
      },
      order: 'post',
    },
    enforce: 'post',
    name: 'vite:archiver',
  };
};

/**
 * 流式压缩指定目录为 zip 文件
 */
async function zipFolder(folderPath: string, outputPath: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const output = fs.createWriteStream(outputPath);
    const archive = archiver('zip', {
      zlib: { level: 9 }, // 最高压缩率
    });

    output.on('close', () => {
      console.log(`ZIP file created: ${outputPath} (${archive.pointer()} total bytes)`);
      resolve();
    });
    output.on('error', reject);

    archive.on('error', (err) => {
      reject(err);
    });

    archive.pipe(output);

    // 以流方式压缩目录，减少内存占用
    archive.directory(folderPath, false);

    archive.finalize();
  });
}
