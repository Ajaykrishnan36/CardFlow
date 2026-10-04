// build check: dummy sync commit
import React from 'react';
import { AuthProvider } from './context/AuthContext';
import { CrmProvider } from './context/CrmContext';
import { AppNavigator } from './navigation/AppNavigator';

export function App() {
  return (
    <AuthProvider>
      <CrmProvider>
        <AppNavigator />
      </CrmProvider>
    </AuthProvider>
  );
}
export default App;
