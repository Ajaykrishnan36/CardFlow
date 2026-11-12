// Everything the website says, in one place, so the words can be changed without touching
// the layout. Kept to what the CRM really does today: no invented numbers or customers.
import type { LucideIcon } from 'lucide-react';
import {
  Bell, Boxes, Briefcase, Building2, Car, ChartColumn, CircleCheck, ClipboardCheck, Cpu, Factory, FileText,
  GraduationCap, Handshake, HardHat, Hotel, Inbox, IndianRupee, Landmark, PackageCheck, Phone, ReceiptText,
  Repeat2, Scale, Scissors, ShieldCheck, Sprout, Stethoscope, Store, Target, Truck, UserCheck, UserPlus, Users, Warehouse
} from 'lucide-react';

import dashboard from '../assets/images/screen-dashboard.webp';
import pipeline from '../assets/images/screen-pipeline.webp';
import leads from '../assets/images/screen-leads.webp';
import lead from '../assets/images/screen-lead.webp';
import forecast from '../assets/images/screen-forecast.webp';
import customers from '../assets/images/screen-customers.webp';
import quote from '../assets/images/screen-quote.webp';
import catalog from '../assets/images/screen-catalog.webp';
import stock from '../assets/images/screen-stock.webp';
import tasks from '../assets/images/screen-tasks.webp';
import invoice from '../assets/images/screen-invoice.webp';
import team from '../assets/images/screen-team.webp';
import opportunities from '../assets/images/screen-opportunities.webp';

/** Real screens from a demonstration business in the CRM (sample data). */
export const screens = { dashboard, pipeline, leads, lead, forecast, customers, quote, catalog, stock, tasks, invoice, team, opportunities };

export const nav = [
  { href: '#top', label: 'Home' },
  { href: '#features', label: 'Features' },
  { href: '#journey', label: 'Solutions' },
  { href: '#industries', label: 'Industries' },
  { href: '#team', label: 'How it works' },
  { href: '#contact', label: 'Contact' }
];

export const before = [
  'Customer details written in notebooks',
  'Leads scattered across many Excel files',
  'Enquiries missed in WhatsApp chats',
  'Salespeople forget follow-ups',
  'Nobody clearly owns a lead',
  'Quotations typed by hand, every time',
  'No idea which quotations are still waiting',
  'Customer history leaves when an employee leaves',
  'No clear view of sales performance',
  'Stock kept in a separate register',
  'Payment reminders forgotten',
  'Reports made by hand at month end',
  'The owner has to ask everyone for updates'
];

export const after = [
  'Every lead stored in one place',
  'Every enquiry assigned to the right person',
  'Every follow-up scheduled and tracked',
  'Every customer has a complete history',
  'Quotations made and tracked in the CRM',
  'Every opportunity visible at its stage',
  'Team performance in real time',
  'Sales targets you can watch',
  'Products and stock organised',
  'Orders, invoices and payments connected',
  'One clear dashboard for the owner'
];

export interface JourneyStep {
  icon: LucideIcon;
  title: string;
  text: string;
  screen: keyof typeof screens;
}

export const journey: JourneyStep[] = [
  { icon: Inbox, title: 'Customer enquiry', text: 'A call, a walk-in, a website form or a visiting card.', screen: 'leads' },
  { icon: UserPlus, title: 'Lead capture', text: 'Saved as a lead in seconds, with its source.', screen: 'leads' },
  { icon: UserCheck, title: 'Lead assignment', text: 'Given to the right salesperson, who is told at once.', screen: 'lead' },
  { icon: Bell, title: 'Salesperson follow-up', text: 'A task with a date, so the call is never forgotten.', screen: 'tasks' },
  { icon: Phone, title: 'Customer interaction', text: 'Calls, notes and messages kept on the record.', screen: 'lead' },
  { icon: Target, title: 'Opportunity creation', text: 'A serious lead becomes a deal with a value and stage.', screen: 'opportunities' },
  { icon: FileText, title: 'Quotation generation', text: 'Products, price, discount and tax in one quotation.', screen: 'quote' },
  { icon: ClipboardCheck, title: 'Customer approval', text: 'Accepted, or a new version with a better price.', screen: 'quote' },
  { icon: PackageCheck, title: 'Order confirmation', text: 'The accepted quotation becomes the order.', screen: 'quote' },
  { icon: ReceiptText, title: 'Invoice generation', text: 'A tax invoice from the order, ready to print.', screen: 'invoice' },
  { icon: IndianRupee, title: 'Payment tracking', text: 'Payments, part payments and the balance due.', screen: 'invoice' },
  { icon: Handshake, title: 'Customer relationship', text: 'Health of every account, from all its activity.', screen: 'customers' },
  { icon: Repeat2, title: 'Repeat business', text: 'Service, renewals and the next sale, on time.', screen: 'dashboard' }
];

export interface Feature {
  id: string;
  eyebrow: string;
  title: string;
  text: string;
  points: string[];
  screen: keyof typeof screens;
  alt: string;
  note?: string;
}

export const features: Feature[] = [
  {
    id: 'leads', eyebrow: 'Lead management', title: 'Every enquiry, in one list you can trust',
    text: 'Write a lead once and it is safe. Search it in a second, see where it came from and who is working on it.',
    points: ['Capture leads from calls, walk-ins, cards and your website', 'Add leads by hand in a few taps', 'Assign each lead to a salesperson', 'Track the source, status and priority', 'Search, filter and sort the whole list', 'Every call, note and message kept on the lead'],
    screen: 'leads', alt: 'Leads list in the CRM with name, source, status and owner for each lead (sample data)'
  },
  {
    id: 'followups', eyebrow: 'Follow-up management', title: 'No more "I forgot to call them back"',
    text: 'Every promise to a customer becomes a task with a date. Your team sees what is due today; you see what is overdue.',
    points: ['Schedule follow-ups and call reminders', 'Overdue follow-ups stand out', 'Each salesperson’s follow-up history', 'Managers see their team’s pending work', 'The next step is always written down', 'Email reminders where email is connected'],
    screen: 'tasks', alt: 'Tasks list showing follow-ups with status, due date and priority (sample data)'
  },
  {
    id: 'pipeline', eyebrow: 'Sales pipeline', title: 'See every deal and where it stands',
    text: 'Qualified leads become opportunities with a value, a closing date and a probability. The pipeline shows what is moving and what is stuck.',
    points: ['Move deals through your own stages', 'Track deal value and expected closing date', 'Probability for every opportunity', 'Spot hot and stalled deals', 'Closed won and closed lost, with the reason', 'Forecast what this quarter can bring in'],
    screen: 'pipeline', alt: 'Sales pipeline with a stage funnel, win rate and the list of open deals (sample data)'
  },
  {
    id: 'team', eyebrow: 'Team & employees', title: 'Your whole team, one way of working',
    text: 'Add people by mobile number and give each one a role. They see their own work; managers see their team; you see everything.',
    points: ['Add team members in minutes', 'Assign leads and opportunities', 'See pending and completed follow-ups', 'Track each person’s performance', 'Role-based access and permission sets', 'Owner and manager visibility'],
    screen: 'team', alt: 'Users and access page listing team members with their roles and status (sample data)'
  },
  {
    id: 'targets', eyebrow: 'Targets & performance', title: 'Know if you will hit the target, before the month ends',
    text: 'Set a target, and the CRM shows what is closed, what is likely, and who on the team is ahead.',
    points: ['Monthly and quarterly targets', 'Targets for each salesperson and the team', 'Target versus achievement', 'Revenue progress and win rate', 'Leaderboard of the team', 'Weekly trend of closed deals'],
    screen: 'forecast', alt: 'Forecast page with a target gauge, path to target chart and team leaderboard (sample data)'
  },
  {
    id: 'stock', eyebrow: 'Products & stock', title: 'Products, prices and stock in the same place',
    text: 'Keep your catalogue and prices, and see what is on hand, promised and running low.',
    points: ['Product catalogue with categories and prices', 'Price books for different customers', 'Stock in and stock out', 'On hand, promised and low-stock alerts', 'Pick products straight into a quotation', 'Stock that follows your orders'],
    screen: 'stock', alt: 'Stock movements page with on-hand stock, value and products running low (sample data)',
    note: 'Stock features depend on the modules switched on for your business.'
  },
  {
    id: 'billing', eyebrow: 'Orders, invoices & payments', title: 'From the approved quotation to the money in the bank',
    text: 'An accepted quotation becomes the order and the invoice. Record payments or a payment plan and always know what is still due.',
    points: ['Orders from approved quotations', 'Tax invoices with your GSTIN', 'Payments, part payments and plans', 'Pending balance on every invoice', 'Payment history for each customer', 'Outstanding amounts at a glance'],
    screen: 'invoice', alt: 'Invoice record showing items, tax, total and payment status (sample data)'
  },
  {
    id: 'reports', eyebrow: 'Dashboard & reports', title: 'Understand your business without asking anyone',
    text: 'Leads, deals, revenue, quotations and payments on one screen, worked out from what your team actually did.',
    points: ['Total and new leads', 'Open and won opportunities', 'Revenue and win rate', 'Deals at risk and pending quotations', 'Salesperson performance', 'Your own reports and charts'],
    screen: 'dashboard', alt: 'Owner dashboard with pipeline flow, win rate, open pipeline value and key numbers (sample data)'
  }
];

export const quoteSteps = [
  'Open the customer or the opportunity',
  'Pick products or services from your catalogue',
  'Add quantity; the price comes from your price book',
  'Apply a discount and the tax set for your business',
  'Print or download a professional quotation',
  'Share it with the customer',
  'Track whether it is sent, accepted or declined',
  'Follow up on the quotations still waiting',
  'Turn the accepted quotation into an order'
];

export const teamSteps = [
  { who: 'Customer', text: 'Sends an enquiry or calls your business.' },
  { who: 'CRM', text: 'Captures it as a lead with its source.' },
  { who: 'Owner / manager', text: 'Assigns the lead to a salesperson.' },
  { who: 'Salesperson', text: 'Gets the lead and a notification.' },
  { who: 'Salesperson', text: 'Calls the customer.' },
  { who: 'Salesperson', text: 'Writes down what was discussed.' },
  { who: 'CRM', text: 'Keeps the next follow-up with its date.' },
  { who: 'Manager', text: 'Sees which follow-ups are done or late.' },
  { who: 'Customer', text: 'Receives the quotation.' },
  { who: 'Salesperson', text: 'Follows up until the deal is won or lost.' }
];

export const roles = [
  { icon: Building2, role: 'Business owner', sees: ['Every lead, deal and customer', 'The whole team’s performance', 'Revenue, targets and payments', 'Who can see and do what'], screen: 'dashboard' as const },
  { icon: Users, role: 'Sales manager', sees: ['Their team’s leads and deals', 'Overdue follow-ups', 'Pipeline and forecast', 'Assigning and reassigning work'], screen: 'forecast' as const },
  { icon: Briefcase, role: 'Salesperson', sees: ['Their own leads and tasks', 'Today’s follow-ups', 'Quotations for their customers', 'Their own target'], screen: 'tasks' as const },
  { icon: ShieldCheck, role: 'Other team members', sees: ['Only what their role allows', 'Service, stock or billing work', 'Notes and history they need', 'Nothing they shouldn’t'], screen: 'team' as const }
];

export interface IndustryCase {
  key: string;
  label: string;
  icon: LucideIcon;
  text: string;
  flow: string[];
}

export const industries: IndustryCase[] = [
  { key: 'retail', label: 'Retail', icon: Store, text: 'Manage customer enquiries, product availability, quotations, orders and repeat customers.', flow: ['Walk-in or phone enquiry', 'Check stock', 'Quotation', 'Order & invoice', 'Repeat purchase reminder'] },
  { key: 'wholesale', label: 'Wholesale & distribution', icon: Warehouse, text: 'Handle dealer enquiries, bulk quotations with price books, orders and outstanding payments.', flow: ['Dealer enquiry', 'Price book quotation', 'Sales order', 'Dispatch', 'Collection follow-up'] },
  { key: 'automotive', label: 'Automotive', icon: Car, text: 'Manage vehicle enquiries, test rides, follow-ups, quotations, bookings and sales.', flow: ['Vehicle enquiry', 'Test ride', 'Quotation & finance', 'Booking', 'Delivery & service'] },
  { key: 'manufacturing', label: 'Manufacturing', icon: Factory, text: 'Manage customer enquiries, product quotations, order tracking and customer relationships.', flow: ['RFQ enquiry', 'Costed quotation', 'Order confirmation', 'Invoice', 'Repeat order'] },
  { key: 'agriculture', label: 'Agriculture', icon: Sprout, text: 'Track farmer and buyer enquiries, product orders, seasonal follow-ups and payments.', flow: ['Farmer / buyer enquiry', 'Product quotation', 'Order', 'Payment', 'Next-season follow-up'] },
  { key: 'transport', label: 'Transport & logistics', icon: Truck, text: 'Handle rate enquiries, route quotations, bookings and invoice follow-ups.', flow: ['Rate enquiry', 'Route quotation', 'Booking', 'Delivery', 'Invoice & payment'] },
  { key: 'services', label: 'Services', icon: Briefcase, text: 'Manage service enquiries, estimates, scheduled visits and service contracts.', flow: ['Service enquiry', 'Estimate', 'Appointment', 'Job done', 'Contract renewal'] },
  { key: 'realestate', label: 'Real estate', icon: Building2, text: 'Manage property enquiries, site visits, follow-ups, opportunities and bookings.', flow: ['Property enquiry', 'Site visit', 'Negotiation', 'Booking', 'Payment schedule'] },
  { key: 'construction', label: 'Construction', icon: HardHat, text: 'Track project enquiries, estimates, client approvals and staged payments.', flow: ['Project enquiry', 'Site survey', 'Estimate', 'Work order', 'Stage payments'] },
  { key: 'education', label: 'Education', icon: GraduationCap, text: 'Manage admission enquiries, counsellor assignments, follow-ups and enrolments.', flow: ['Admission enquiry', 'Counsellor assigned', 'Campus visit', 'Enrolment', 'Fee collection'] },
  { key: 'healthcare', label: 'Healthcare', icon: Stethoscope, text: 'Handle patient enquiries, appointments, follow-up visits and packages.', flow: ['Patient enquiry', 'Appointment', 'Consultation', 'Package quotation', 'Follow-up visit'] },
  { key: 'hospitality', label: 'Hospitality', icon: Hotel, text: 'Manage booking and event enquiries, quotations, confirmations and repeat guests.', flow: ['Booking enquiry', 'Package quotation', 'Confirmation', 'Stay / event', 'Return visit offer'] },
  { key: 'technology', label: 'Technology', icon: Cpu, text: 'Track software and service leads, demos, proposals, subscriptions and renewals.', flow: ['Inbound lead', 'Demo', 'Proposal', 'Subscription', 'Renewal'] },
  { key: 'finance', label: 'Finance', icon: Landmark, text: 'Manage loan and investment enquiries, documents, applications and follow-ups.', flow: ['Enquiry', 'Documents collected', 'Application', 'Approval', 'Repayment follow-up'] },
  { key: 'insurance', label: 'Insurance', icon: ShieldCheck, text: 'Track policy enquiries, quotations, renewals and claims follow-ups.', flow: ['Policy enquiry', 'Plan comparison', 'Policy issued', 'Renewal reminder', 'Claims support'] },
  { key: 'legal', label: 'Legal services', icon: Scale, text: 'Manage client enquiries, consultations, matters, hearings and fees.', flow: ['Client enquiry', 'Consultation', 'Matter opened', 'Hearing dates', 'Fee invoice'] },
  { key: 'beauty', label: 'Beauty & salon', icon: Scissors, text: 'Manage client enquiries, appointments, memberships and repeat visits.', flow: ['Enquiry', 'Appointment', 'Service done', 'Membership offer', 'Next visit reminder'] },
  { key: 'other', label: 'Other businesses', icon: ChartColumn, text: 'Any business that gets enquiries and sells to customers: start with the common CRM and your own words.', flow: ['Enquiry', 'Follow-up', 'Quotation', 'Sale', 'Repeat business'] }
];

export const benefits = [
  { icon: Inbox, title: 'No more scattered customer information' },
  { icon: Bell, title: 'No more forgotten follow-ups' },
  { icon: Phone, title: 'No more lost enquiries' },
  { icon: UserCheck, title: 'Better team accountability' },
  { icon: FileText, title: 'Faster quotations' },
  { icon: ChartColumn, title: 'Clear sales visibility' },
  { icon: Handshake, title: 'Better customer relationships' },
  { icon: Boxes, title: 'Better stock visibility' },
  { icon: CircleCheck, title: 'More organised operations' },
  { icon: Target, title: 'Decisions based on real numbers' }
];

export const showcase = [
  { key: 'dashboard', label: 'Owner dashboard' },
  { key: 'leads', label: 'Lead management' },
  { key: 'pipeline', label: 'Sales pipeline' },
  { key: 'opportunities', label: 'Opportunities' },
  { key: 'tasks', label: 'Follow-ups' },
  { key: 'quote', label: 'Quotation' },
  { key: 'catalog', label: 'Product catalogue' },
  { key: 'stock', label: 'Inventory' },
  { key: 'forecast', label: 'Team performance' },
  { key: 'customers', label: 'Customer health' }
] as const;

export const faqs = [
  { q: 'What is CRM software?', a: 'CRM stands for customer relationship management. It is one place where your business keeps every enquiry, customer, follow-up, quotation and sale, instead of notebooks, Excel files and memory.' },
  { q: 'How does a CRM help a small business?', a: 'Every enquiry is saved, every follow-up has a date and an owner, and you can see what is pending without asking anyone. Small teams feel the difference quickly because nothing depends on one person remembering.' },
  { q: 'Can I manage all my leads in one place?', a: 'Yes. Leads you add by hand, leads from visiting cards and enquiries from your website all land in the same list, with their source.' },
  { q: 'Can I assign leads to my sales team?', a: 'Yes. Each lead has an owner. You or your manager can assign and reassign leads, and the salesperson is notified.' },
  { q: 'How does follow-up management work?', a: 'Each follow-up is a task with a date and a person. Your team sees what is due today, and overdue follow-ups stand out for the manager.' },
  { q: 'Can I create and share quotations?', a: 'Yes. Pick products from your catalogue, the price comes from your price book, add discounts and tax, then print or download the quotation to share it. A changed price becomes a new version.' },
  { q: 'Can I track sales targets?', a: 'Yes. The forecast shows closed deals against the target for the period, with the team leaderboard and the trend week by week.' },
  { q: 'Does the CRM support inventory?', a: 'Yes, when the stock modules are switched on for your business: products, stock in and out, on-hand quantities and low-stock alerts.' },
  { q: 'Can I use the CRM for my industry?', a: 'When you sign up you choose your industry, and the CRM comes with the right fields and words for it. Retail, automotive, real estate, education, legal, insurance and many more are supported.' },
  { q: 'Can I manage multiple employees?', a: 'Yes. Add your team by mobile number, give each person a role, and decide what each role can see and do.' },
  { q: 'Can I use the CRM on my mobile?', a: 'Yes. It works in the browser on phones, tablets and computers, and the screens fit the device you are using.' },
  { q: 'How can I request a demo?', a: 'Use Book a Free Call on this page. Tell us a little about your business and a good time, and our team will call you to confirm.' },
  { q: 'How do I get started?', a: 'Sign up with your mobile number and your business details. Once your business is approved you get its own private address and can add your team.' }
];
