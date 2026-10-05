-- ==============================================================================
-- 002_seed_data.sql: reference data only — the directory's categories.
-- ==============================================================================

-- 1. Seed Categories (Top 20 Categories from PRD Appendix B)
INSERT INTO categories (id, name, slug, icon, sort_order) VALUES
('00000000-0000-0000-0000-0000000000c1', 'Manufacturing', 'manufacturing', 'Factory', 1),
('00000000-0000-0000-0000-0000000000c2', 'IT & Software', 'it-software', 'Code', 2),
('00000000-0000-0000-0000-0000000000c3', 'Textiles & Garments', 'textiles', 'Shirt', 3),
('00000000-0000-0000-0000-0000000000c4', 'Hardware & Building Materials', 'hardware', 'Wrench', 4),
('00000000-0000-0000-0000-0000000000c5', 'Electrical & Automation', 'electrical', 'Zap', 5),
('00000000-0000-0000-0000-0000000000c6', 'Construction & Civil', 'construction', 'Building2', 6),
('00000000-0000-0000-0000-0000000000c7', 'Plumbing & Sanitation', 'plumbing', 'Droplet', 7),
('00000000-0000-0000-0000-0000000000c8', 'Logistics & Transport', 'logistics', 'Truck', 8),
('00000000-0000-0000-0000-0000000000c9', 'Consultants (CA/Legal/HR)', 'consultants', 'Briefcase', 9),
('00000000-0000-0000-0000-0000000000ca', 'Printing & Packaging', 'printing', 'Printer', 10),
('00000000-0000-0000-0000-0000000000cb', 'Interiors & Furniture', 'interiors', 'Armchair', 11),
('00000000-0000-0000-0000-0000000000cc', 'Auto (Sales & Service)', 'auto', 'Car', 12),
('00000000-0000-0000-0000-0000000000cd', 'Health & Clinics', 'health', 'Stethoscope', 13),
('00000000-0000-0000-0000-0000000000ce', 'Education & Training', 'education', 'GraduationCap', 14),
('00000000-0000-0000-0000-0000000000cf', 'Food & Catering', 'food', 'Utensils', 15),
('00000000-0000-0000-0000-0000000000d0', 'Events & Photography', 'events', 'Camera', 16),
('00000000-0000-0000-0000-0000000000d1', 'Real Estate', 'real-estate', 'Home', 17),
('00000000-0000-0000-0000-0000000000d2', 'Security & CCTV', 'security', 'Shield', 18),
('00000000-0000-0000-0000-0000000000d3', 'Cleaning & Pest Control', 'cleaning', 'Sparkles', 19),
('00000000-0000-0000-0000-0000000000d4', 'Repairs & AMC', 'repairs', 'Tool', 20)
ON CONFLICT (slug) DO NOTHING;

-- No people or businesses are seeded here. Sample accounts used to live in this file;
-- the baseline customer now comes from the CRM's fresh start (crm/freshstart.go, D-106).
