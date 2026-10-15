declare module '*.css';

/** Build-time settings put in by webpack's DefinePlugin (see webpack.config.js). */
declare const process: { env: { CRM_API_ORIGIN?: string } };
