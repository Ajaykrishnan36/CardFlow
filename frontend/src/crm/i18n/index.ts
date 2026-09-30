import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import en from './en';
import products from './products';
import workspaces from './workspaces';
import records from './records';
import users from './users';
import audit from './audit';
import invite from './invite';
import access from './access';
import workspaceApp from './workspaceApp';
import integrations from './integrations';
import objects from './objects';
import reports from './reports';
import lists from './lists';
import extras from './extras';
import tools from './tools';

// Feature strings live in their own files and are merged under their feature key,
// e.g. t('products.list.title').
void i18n.use(initReactI18next).init({
  resources: { en: { crm: { ...en, ...extras, products, workspaces, records, users, audit, invite, access, workspaceApp, integrations, objects, reports, lists, tools } } },
  lng: 'en',
  fallbackLng: 'en',
  defaultNS: 'crm',
  ns: ['crm'],
  interpolation: { escapeValue: false }, // React already escapes output
  returnNull: false
});

export default i18n;
