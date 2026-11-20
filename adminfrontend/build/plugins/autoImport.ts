import type {PluginOption} from 'vite';
import path from 'path';
import AutoImport from 'unplugin-auto-import/vite';

/**
 * 自动导入处理
 */
export const autoImportPlugin = (): PluginOption => {
    return AutoImport({
        dirs: [
            path.resolve(__dirname, '../../src/hooks/**'),
            path.resolve(__dirname, '../../src/components/**'),
            path.resolve(__dirname, '../../src/stores/**'),
            path.resolve(__dirname, '../../types/**'),
        ],
        imports: [
            'react',
            'react-router',
            'react-router-dom',
            'react-i18next',
            {from: 'react', imports: ['FC'], type: true},
        ],
        dts: path.resolve(__dirname, '../../types/autoImports.d.ts'),
        include: [/\.[tj]sx?$/],
        resolvers: [
            (name) => {
                // 处理 @/ 开头的路径别名
                if (name.startsWith('@/')) {
                    return {
                        from: name.replace('@/', path.resolve(__dirname, 'src/') + '/'),
                    };
                }
            },
        ],
    });
};
