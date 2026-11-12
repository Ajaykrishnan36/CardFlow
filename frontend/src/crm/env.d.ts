declare module '*.css';

/** Build-time settings put in by webpack's DefinePlugin (see webpack.config.js). */
declare const process: { env: { CRM_API_ORIGIN?: string } };

/** Images and video bundled by webpack (asset/resource): the import is the file's URL. */
declare module '*.webp' { const url: string; export default url; }
declare module '*.png' { const url: string; export default url; }
declare module '*.jpg' { const url: string; export default url; }
declare module '*.mp4' { const url: string; export default url; }
declare module '*.webm' { const url: string; export default url; }
