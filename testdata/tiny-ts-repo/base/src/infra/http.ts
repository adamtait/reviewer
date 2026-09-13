export type Request = {
  url: string;
  body: string;
};

export async function send(request: Request): Promise<void> {
  void request;
}
