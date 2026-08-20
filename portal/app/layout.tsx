import "./globals.css";

export const metadata = {
  title: "Scholaroscope Timetable Portal",
  description: "Temporal timetable management portal",
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
