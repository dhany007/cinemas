import { CrudWorkstation } from "@/components/crud-workstation";
export default function CinemasPage(){return <CrudWorkstation title="Bioskop" endpoint="/admin/cinemas" responseKey="cinemas" fields={[{name:"name",label:"Nama"},{name:"address",label:"Alamat"},{name:"city",label:"Kota"}]}/>}
